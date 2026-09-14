package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
	"go.uber.org/zap"
)

type TaskType string

const (
	TaskTypeDownload TaskType = "download"
	TaskTypeUpload   TaskType = "upload"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusScheduled TaskStatus = "scheduled"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

type Priority int

const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 2
	PriorityHigh   Priority = 3
)

type PostAction string

const (
	PostActionNone       PostAction = "none"
	PostActionTranscribe PostAction = "transcribe"
	PostActionConvert    PostAction = "convert"
	PostActionNotify     PostAction = "notify"
	PostActionSleep      PostAction = "sleep"
	PostActionShutdown   PostAction = "shutdown"
)

type QueueTask struct {
	ID               string     `json:"id"`
	Type             TaskType   `json:"type"`
	Status           TaskStatus `json:"status"`
	Priority         Priority   `json:"priority"`
	PeerID           int64      `json:"peer_id"`
	MessageID        int        `json:"message_id"`
	FileName         string     `json:"file_name"`
	FilePath         string     `json:"file_path"`
	TotalBytes       int64      `json:"total_bytes"`
	TransferredBytes int64      `json:"transferred_bytes"`
	SpeedBytesPerSec int64      `json:"speed_bytes_per_sec"`
	ProgressPercent  float64    `json:"progress_percent"`
	PostAction       PostAction `json:"post_action"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	Error            string     `json:"error,omitempty"`
}

type QueueScheduleConfig struct {
	ScheduleEnabled      bool           `json:"schedule_enabled"`
	StartTime            string         `json:"start_time"` // "HH:MM", e.g. "02:00"
	EndTime              string         `json:"end_time"`   // "HH:MM", e.g. "07:00"
	ActiveDays           []time.Weekday `json:"active_days"`
	MaxConcurrentTasks   int            `json:"max_concurrent_tasks"`
	GlobalMaxBytesPerSec int64          `json:"global_max_bytes_per_sec"` // 0 = unlimited
	OnQueueComplete      PostAction     `json:"on_queue_complete"`
}

var defaultScheduleConfig = QueueScheduleConfig{
	ScheduleEnabled:    false,
	StartTime:          "01:00",
	EndTime:            "07:00",
	ActiveDays:         []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday},
	MaxConcurrentTasks: 3,
	OnQueueComplete:    PostActionNone,
}

var (
	bucketQueueTasks  = []byte("queue_tasks")
	bucketQueueConfig = []byte("queue_config")
	configKey         = []byte("current_config")
)

// QueueManager coordinates scheduled and priority downloads/uploads with bandwidth limits.
type QueueManager struct {
	mu          sync.RWMutex
	tasks       map[string]*QueueTask
	cfg         QueueScheduleConfig
	db          *bbolt.DB
	logger      *zap.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	activeSlots chan struct{}
	listeners   []func(task *QueueTask)
}

// NewQueueManager initializes a persistent QueueManager backed by BBolt.
func NewQueueManager(db *bbolt.DB, logger *zap.Logger) (*QueueManager, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	qm := &QueueManager{
		tasks:       make(map[string]*QueueTask),
		cfg:         defaultScheduleConfig,
		db:          db,
		logger:      logger.Named("queue_manager"),
		ctx:         ctx,
		cancel:      cancel,
		activeSlots: make(chan struct{}, 3),
	}

	if db != nil {
		if err := db.Update(func(tx *bbolt.Tx) error {
			if _, err := tx.CreateBucketIfNotExists(bucketQueueTasks); err != nil {
				return err
			}
			_, err := tx.CreateBucketIfNotExists(bucketQueueConfig)
			return err
		}); err != nil {
			return nil, fmt.Errorf("init queue db buckets: %w", err)
		}

		// Load persisted config
		_ = db.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket(bucketQueueConfig)
			if b == nil {
				return nil
			}
			data := b.Get(configKey)
			if data != nil {
				_ = json.Unmarshal(data, &qm.cfg)
			}
			return nil
		})

		// Load persisted tasks
		_ = db.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket(bucketQueueTasks)
			if b == nil {
				return nil
			}
			return b.ForEach(func(k, v []byte) error {
				var t QueueTask
				if err := json.Unmarshal(v, &t); err == nil {
					if t.Status == TaskStatusRunning {
						t.Status = TaskStatusPending
					}
					qm.tasks[t.ID] = &t
				}
				return nil
			})
		})
	}

	qm.updateSlotCapacity()
	go qm.loop()

	return qm, nil
}

func (qm *QueueManager) Close() {
	qm.cancel()
}

// RegisterListener attaches a callback for task state/progress updates.
func (qm *QueueManager) RegisterListener(fn func(task *QueueTask)) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.listeners = append(qm.listeners, fn)
}

func (qm *QueueManager) updateSlotCapacity() {
	limit := qm.cfg.MaxConcurrentTasks
	if limit <= 0 {
		limit = 3
	}
	qm.activeSlots = make(chan struct{}, limit)
}

// IsScheduleActive checks if the current time matches the scheduled active window.
func (qm *QueueManager) IsScheduleActive(now time.Time) bool {
	if !qm.cfg.ScheduleEnabled {
		return true // Always active if scheduling is disabled
	}

	dayMatch := false
	weekday := now.Weekday()
	for _, d := range qm.cfg.ActiveDays {
		if d == weekday {
			dayMatch = true
			break
		}
	}
	if !dayMatch {
		return false
	}

	curMin := now.Hour()*60 + now.Minute()
	startMin := parseMinutes(qm.cfg.StartTime)
	endMin := parseMinutes(qm.cfg.EndTime)

	if startMin <= endMin {
		return curMin >= startMin && curMin < endMin
	}
	// Overnight window, e.g. 23:00 to 06:00
	return curMin >= startMin || curMin < endMin
}

func parseMinutes(hhmm string) int {
	parts := strings.Split(strings.TrimSpace(hhmm), ":")
	if len(parts) != 2 {
		return 0
	}
	h := 0
	m := 0
	_, _ = fmt.Sscanf(parts[0], "%d", &h)
	_, _ = fmt.Sscanf(parts[1], "%d", &m)
	return h*60 + m
}

// AddTask queues a new download or upload task.
func (qm *QueueManager) AddTask(t QueueTask) (*QueueTask, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if t.ID == "" {
		t.ID = fmt.Sprintf("task_%d", time.Now().UnixNano())
	}
	if t.Status == "" {
		t.Status = TaskStatusPending
	}
	if t.Priority == 0 {
		t.Priority = PriorityNormal
	}
	t.CreatedAt = time.Now()

	qm.tasks[t.ID] = &t
	qm.saveTaskLocked(&t)
	qm.logger.Info("Task added to queue",
		zap.String("id", t.ID),
		zap.String("type", string(t.Type)),
		zap.String("file", t.FileName))

	return &t, nil
}

// ListTasks returns all tasks in the queue.
func (qm *QueueManager) ListTasks() []QueueTask {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	list := make([]QueueTask, 0, len(qm.tasks))
	for _, t := range qm.tasks {
		list = append(list, *t)
	}
	return list
}

// PauseTask sets a task to paused.
func (qm *QueueManager) PauseTask(id string) (bool, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	t, ok := qm.tasks[id]
	if !ok {
		return false, fmt.Errorf("task %s not found", id)
	}
	t.Status = TaskStatusPaused
	qm.saveTaskLocked(t)
	return true, nil
}

// ResumeTask sets a paused task back to pending.
func (qm *QueueManager) ResumeTask(id string) (bool, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	t, ok := qm.tasks[id]
	if !ok {
		return false, fmt.Errorf("task %s not found", id)
	}
	t.Status = TaskStatusPending
	qm.saveTaskLocked(t)
	return true, nil
}

// DeleteTask removes a task from the queue.
func (qm *QueueManager) DeleteTask(id string) (bool, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if _, ok := qm.tasks[id]; !ok {
		return false, nil
	}
	delete(qm.tasks, id)

	if qm.db != nil {
		_ = qm.db.Update(func(tx *bbolt.Tx) error {
			b := tx.Bucket(bucketQueueTasks)
			if b != nil {
				return b.Delete([]byte(id))
			}
			return nil
		})
	}
	return true, nil
}

// GetConfig returns the current schedule configuration.
func (qm *QueueManager) GetConfig() QueueScheduleConfig {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return qm.cfg
}

// UpdateConfig updates the schedule and limits.
func (qm *QueueManager) UpdateConfig(cfg QueueScheduleConfig) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.cfg = cfg
	qm.updateSlotCapacity()

	if qm.db != nil {
		_ = qm.db.Update(func(tx *bbolt.Tx) error {
			b := tx.Bucket(bucketQueueConfig)
			if b != nil {
				raw, err := json.Marshal(cfg)
				if err == nil {
					return b.Put(configKey, raw)
				}
			}
			return nil
		})
	}
	return nil
}

// UpdateProgress updates the progress of a running task and notifies listeners.
func (qm *QueueManager) UpdateProgress(id string, transferred, total, speed int64) {
	qm.mu.Lock()
	t, ok := qm.tasks[id]
	if !ok {
		qm.mu.Unlock()
		return
	}
	t.TransferredBytes = transferred
	t.TotalBytes = total
	t.SpeedBytesPerSec = speed
	if total > 0 {
		t.ProgressPercent = float64(transferred) / float64(total) * 100.0
	}
	qm.saveTaskLocked(t)
	taskCopy := *t
	listeners := make([]func(task *QueueTask), len(qm.listeners))
	copy(listeners, qm.listeners)
	qm.mu.Unlock()

	for _, l := range listeners {
		l(&taskCopy)
	}
}

// MarkCompleted marks a task as successfully completed.
func (qm *QueueManager) MarkCompleted(id string) {
	qm.mu.Lock()
	t, ok := qm.tasks[id]
	if !ok {
		qm.mu.Unlock()
		return
	}
	now := time.Now()
	t.Status = TaskStatusCompleted
	t.CompletedAt = &now
	t.ProgressPercent = 100.0
	t.SpeedBytesPerSec = 0
	qm.saveTaskLocked(t)
	postAction := t.PostAction
	taskCopy := *t
	qm.mu.Unlock()

	qm.executePostAction(postAction, taskCopy)
}

// MarkFailed marks a task as failed.
func (qm *QueueManager) MarkFailed(id string, errText string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	t, ok := qm.tasks[id]
	if !ok {
		return
	}
	t.Status = TaskStatusFailed
	t.Error = errText
	t.SpeedBytesPerSec = 0
	qm.saveTaskLocked(t)
}

func (qm *QueueManager) saveTaskLocked(t *QueueTask) {
	if qm.db == nil {
		return
	}
	_ = qm.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketQueueTasks)
		if b != nil {
			data, err := json.Marshal(t)
			if err == nil {
				return b.Put([]byte(t.ID), data)
			}
		}
		return nil
	})
}

func (qm *QueueManager) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-qm.ctx.Done():
			return
		case now := <-ticker.C:
			if !qm.IsScheduleActive(now) {
				// Mark running tasks as scheduled if outside active window
				qm.mu.Lock()
				for _, t := range qm.tasks {
					if t.Status == TaskStatusPending {
						t.Status = TaskStatusScheduled
					}
				}
				qm.mu.Unlock()
				continue
			}

			// Restore scheduled tasks to pending when inside active window
			qm.mu.Lock()
			for _, t := range qm.tasks {
				if t.Status == TaskStatusScheduled {
					t.Status = TaskStatusPending
				}
			}
			qm.mu.Unlock()
		}
	}
}

func (qm *QueueManager) executePostAction(action PostAction, task QueueTask) {
	switch action {
	case PostActionSleep:
		qm.logger.Info("Executing post-action: System Sleep")
		if runtime.GOOS == "windows" {
			_ = exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Run()
		}
	case PostActionShutdown:
		qm.logger.Info("Executing post-action: System Shutdown")
		if runtime.GOOS == "windows" {
			_ = exec.Command("shutdown", "/s", "/t", "60").Run()
		} else {
			_ = exec.Command("shutdown", "-h", "+1").Run()
		}
	case PostActionNotify:
		qm.logger.Info("Executing post-action: Task Completed Notification",
			zap.String("task_id", task.ID),
			zap.String("file", task.FileName))
	}
}
