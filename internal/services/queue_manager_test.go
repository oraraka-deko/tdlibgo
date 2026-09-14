package services_test

import (
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
	"go.uber.org/zap"

	"tdlibgo/internal/services"
)

func TestQueueManager_ScheduleActive(t *testing.T) {
	qm, err := services.NewQueueManager(nil, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create QueueManager: %v", err)
	}
	defer qm.Close()

	// Default: scheduling is disabled, should always be active
	now := time.Now()
	if !qm.IsScheduleActive(now) {
		t.Errorf("expected schedule to be active when disabled")
	}

	// Enable schedule with specific time window 02:00 - 05:00 on Monday
	cfg := services.QueueScheduleConfig{
		ScheduleEnabled:    true,
		StartTime:          "02:00",
		EndTime:            "05:00",
		ActiveDays:         []time.Weekday{time.Monday},
		MaxConcurrentTasks: 2,
	}
	if err := qm.UpdateConfig(cfg); err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	// Check Monday 03:00 (inside window)
	monActive := time.Date(2026, 9, 14, 3, 0, 0, 0, time.Local) // 2026-09-14 is Monday
	if !qm.IsScheduleActive(monActive) {
		t.Errorf("expected Monday 03:00 to be active")
	}

	// Check Monday 06:00 (outside window)
	monInactive := time.Date(2026, 9, 14, 6, 0, 0, 0, time.Local)
	if qm.IsScheduleActive(monInactive) {
		t.Errorf("expected Monday 06:00 to be inactive")
	}

	// Check Tuesday 03:00 (inactive day)
	tueInactive := time.Date(2026, 9, 15, 3, 0, 0, 0, time.Local)
	if qm.IsScheduleActive(tueInactive) {
		t.Errorf("expected Tuesday 03:00 to be inactive day")
	}

	// Overnight window: 23:00 to 04:00
	cfg.StartTime = "23:00"
	cfg.EndTime = "04:00"
	_ = qm.UpdateConfig(cfg)

	mon2330 := time.Date(2026, 9, 14, 23, 30, 0, 0, time.Local)
	if !qm.IsScheduleActive(mon2330) {
		t.Errorf("expected Monday 23:30 to be active in overnight window")
	}

	mon0330 := time.Date(2026, 9, 14, 3, 30, 0, 0, time.Local)
	if !qm.IsScheduleActive(mon0330) {
		t.Errorf("expected Monday 03:30 to be active in overnight window")
	}

	mon1200 := time.Date(2026, 9, 14, 12, 0, 0, 0, time.Local)
	if qm.IsScheduleActive(mon1200) {
		t.Errorf("expected Monday 12:00 to be inactive in overnight window")
	}
}

func TestQueueManager_TasksAddAndList(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_queue.db")
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("failed to open bbolt: %v", err)
	}
	defer db.Close()

	qm, err := services.NewQueueManager(db, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create QueueManager: %v", err)
	}
	defer qm.Close()

	// Add Task
	task, err := qm.AddTask(services.QueueTask{
		Type:       services.TaskTypeDownload,
		FileName:   "test_video.mp4",
		TotalBytes: 104857600, // 100MB
		Priority:   services.PriorityHigh,
		PostAction: services.PostActionNotify,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}
	if task.ID == "" {
		t.Errorf("expected task to have an ID assigned")
	}

	// List Tasks
	tasks := qm.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].FileName != "test_video.mp4" {
		t.Errorf("expected file name test_video.mp4, got %s", tasks[0].FileName)
	}

	// Pause Task
	ok, err := qm.PauseTask(task.ID)
	if err != nil || !ok {
		t.Fatalf("failed to pause task: %v", err)
	}

	tasks = qm.ListTasks()
	if tasks[0].Status != services.TaskStatusPaused {
		t.Errorf("expected status paused, got %s", tasks[0].Status)
	}

	// Resume Task
	ok, err = qm.ResumeTask(task.ID)
	if err != nil || !ok {
		t.Fatalf("failed to resume task: %v", err)
	}

	tasks = qm.ListTasks()
	if tasks[0].Status != services.TaskStatusPending {
		t.Errorf("expected status pending, got %s", tasks[0].Status)
	}

	// Update Progress
	qm.UpdateProgress(task.ID, 52428800, 104857600, 1048576) // 50%, 1MB/s
	tasks = qm.ListTasks()
	if tasks[0].ProgressPercent < 49.9 || tasks[0].ProgressPercent > 50.1 {
		t.Errorf("expected 50%% progress, got %f", tasks[0].ProgressPercent)
	}

	// Mark Completed
	qm.MarkCompleted(task.ID)
	tasks = qm.ListTasks()
	if tasks[0].Status != services.TaskStatusCompleted {
		t.Errorf("expected status completed, got %s", tasks[0].Status)
	}

	// Delete Task
	ok, err = qm.DeleteTask(task.ID)
	if err != nil || !ok {
		t.Fatalf("failed to delete task: %v", err)
	}
	if len(qm.ListTasks()) != 0 {
		t.Errorf("expected 0 tasks after delete")
	}
}
