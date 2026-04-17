package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type LogLevel string

const (
	levelDebug LogLevel = "DEBUG"
	levelInfo  LogLevel = "INFO"
	levelWarn  LogLevel = "WARN"
	levelError LogLevel = "ERROR"
)

const (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorBlue   = "\033[34m"
	colorWhite  = "\033[97m"
)

type ColorLogger struct {
	mu    sync.Mutex
	stats *StatsTracker
}

func NewColorLogger(stats *StatsTracker) *ColorLogger {
	return &ColorLogger{stats: stats}
}

func (l *ColorLogger) Debugf(format string, args ...any) {
	l.logf(levelDebug, format, args...)
}

func (l *ColorLogger) Infof(format string, args ...any) {
	l.logf(levelInfo, format, args...)
}

func (l *ColorLogger) Warnf(format string, args ...any) {
	l.logf(levelWarn, format, args...)
}

func (l *ColorLogger) Errorf(format string, args ...any) {
	l.logf(levelError, format, args...)
}

func (l *ColorLogger) logf(level LogLevel, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	message := fmt.Sprintf(format, args...)
	timePart := fmt.Sprintf("%s[%s]%s", colorBlue, now.Format("2006-01-02 15:04:05"), colorReset)
	levelPart := fmt.Sprintf("%s[%s]%s", levelColor(level), level, colorReset)
	messagePart := fmt.Sprintf("%s%s%s", messageColor(level), strings.TrimSpace(message), colorReset)
	fmt.Fprintf(os.Stdout, "%s %s %s\n", timePart, levelPart, messagePart)

	if l.stats != nil {
		l.stats.RecordLog(LogEntry{
			Time:    now,
			Level:   string(level),
			Message: strings.TrimSpace(message),
		})
	}
}

func levelColor(level LogLevel) string {
	switch level {
	case levelDebug:
		return colorCyan
	case levelInfo:
		return colorGreen
	case levelWarn:
		return colorYellow
	case levelError:
		return colorRed
	default:
		return colorReset
	}
}

func messageColor(level LogLevel) string {
	switch level {
	case levelDebug:
		return colorWhite
	case levelInfo:
		return colorCyan
	case levelWarn:
		return colorYellow
	case levelError:
		return colorRed
	default:
		return colorReset
	}
}
