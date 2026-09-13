package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Level defines log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

// ANSI colors for console output.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// Tag colors for visual clarity in logs.
var tagColors = map[string]string{
	"MTProto":  colorCyan,
	"SYNC":     colorGreen,
	"UPLOAD":   colorPurple,
	"DOWNLOAD": colorBlue,
	"RETRY":    colorYellow,
	"HTTP":     colorGray,
	"DB":       colorGreen,
	"ERROR":    colorRed,
	"AUTH":     colorGreen,
}

// LogSubscriber receives formatted log messages (e.g. for WebSocket streaming).
type LogSubscriber func(level string, tag string, message string, timestamp time.Time)

// Logger is a high-visibility, thread-safe logger with console colors, file rotation, and broadcast hooks.
type Logger struct {
	mu          sync.RWMutex
	minLevel    Level
	fileWriter  io.Writer
	subscribers map[uint64]LogSubscriber
	nextSubID   uint64
}

var (
	defaultLogger *Logger
	onceDefault   sync.Once
)

// InitLogger initializes the global logger with a rotating file destination.
func InitLogger(logDir string, filename string) *Logger {
	var fw io.Writer
	if logDir != "" {
		_ = os.MkdirAll(logDir, 0755)
		fw = &lumberjack.Logger{
			Filename:   filepath.Join(logDir, filename),
			MaxSize:    10, // megabytes
			MaxBackups: 5,
			MaxAge:     14, // days
			Compress:   true,
		}
	}

	l := &Logger{
		minLevel:    LevelDebug,
		fileWriter:  fw,
		subscribers: make(map[uint64]LogSubscriber),
	}

	onceDefault.Do(func() {
		defaultLogger = l
	})
	return l
}

// Default returns the default global logger instance.
func Default() *Logger {
	if defaultLogger == nil {
		InitLogger("session", "system.log")
	}
	return defaultLogger
}

// Subscribe registers a listener for real-time log streaming and returns an unsubscribe func.
func (l *Logger) Subscribe(sub LogSubscriber) func() {
	l.mu.Lock()
	l.nextSubID++
	id := l.nextSubID
	l.subscribers[id] = sub
	l.mu.Unlock()

	return func() {
		l.mu.Lock()
		delete(l.subscribers, id)
		l.mu.Unlock()
	}
}

func (l *Logger) log(lvl Level, tag string, format string, args ...any) {
	if lvl < l.minLevel {
		return
	}

	now := time.Now()
	timeStr := now.Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	lvlName := levelNames[lvl]

	// Console formatted line with color
	tagColor, ok := tagColors[tag]
	if !ok {
		tagColor = colorBold
	}

	var lvlColor string
	switch lvl {
	case LevelDebug:
		lvlColor = colorGray
	case LevelInfo:
		lvlColor = colorGreen
	case LevelWarn:
		lvlColor = colorYellow
	case LevelError:
		lvlColor = colorRed
	}

	consoleLine := fmt.Sprintf("%s%s%s %s[%-5s]%s %s[%-8s]%s %s\n",
		colorGray, timeStr, colorReset,
		lvlColor, lvlName, colorReset,
		tagColor, tag, colorReset,
		msg,
	)

	// Plain line for file
	plainLine := fmt.Sprintf("%s [%-5s] [%-8s] %s\n", timeStr, lvlName, tag, msg)

	// Write to console
	_, _ = fmt.Fprint(os.Stdout, consoleLine)

	// Write to rotating file
	l.mu.RLock()
	if l.fileWriter != nil {
		_, _ = fmt.Fprint(l.fileWriter, plainLine)
	}
	subs := make([]LogSubscriber, 0, len(l.subscribers))
	for _, sub := range l.subscribers {
		subs = append(subs, sub)
	}
	l.mu.RUnlock()

	// Notify WebSocket / external subscribers
	for _, sub := range subs {
		sub(lvlName, tag, msg, now)
	}
}

// Helper methods for tagged logging

func MTProto(format string, args ...any)  { Default().log(LevelInfo, "MTProto", format, args...) }
func Sync(format string, args ...any)     { Default().log(LevelInfo, "SYNC", format, args...) }
func Upload(format string, args ...any)   { Default().log(LevelInfo, "UPLOAD", format, args...) }
func Download(format string, args ...any) { Default().log(LevelInfo, "DOWNLOAD", format, args...) }
func Retry(format string, args ...any)    { Default().log(LevelWarn, "RETRY", format, args...) }
func DB(format string, args ...any)       { Default().log(LevelDebug, "DB", format, args...) }
func HTTP(format string, args ...any)     { Default().log(LevelInfo, "HTTP", format, args...) }
func Error(tag string, format string, args ...any) {
	Default().log(LevelError, tag, format, args...)
}
func Info(tag string, format string, args ...any) {
	Default().log(LevelInfo, tag, format, args...)
}
func Warn(tag string, format string, args ...any) {
	Default().log(LevelWarn, tag, format, args...)
}
