package kubefs

import (
	"log"
	"strings"
	"sync/atomic"
)

type LogLevel int32

const (
	LevelTrace LogLevel = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

var logLevel int32 = int32(LevelInfo)

func SetLogLevel(level string) {
	if strings.TrimSpace(level) == "" {
		level = "info"
	}

	parsed, ok := parseLogLevel(level)
	if !ok {
		log.Printf("[WARN] Unknown log level %q, defaulting to info", level)
		parsed = LevelInfo
	}

	atomic.StoreInt32(&logLevel, int32(parsed))
}

func Tracef(format string, args ...interface{}) {
	logf(LevelTrace, "TRACE", format, args...)
}

func Debugf(format string, args ...interface{}) {
	logf(LevelDebug, "DEBUG", format, args...)
}

func Infof(format string, args ...interface{}) {
	logf(LevelInfo, "INFO", format, args...)
}

func Warnf(format string, args ...interface{}) {
	logf(LevelWarn, "WARN", format, args...)
}

func Errorf(format string, args ...interface{}) {
	logf(LevelError, "ERROR", format, args...)
}

func Fatalf(format string, args ...interface{}) {
	log.Fatalf("[FATAL] "+format, args...)
}

func logf(level LogLevel, label string, format string, args ...interface{}) {
	if !shouldLog(level) {
		return
	}
	log.Printf("[%s] "+format, append([]interface{}{label}, args...)...)
}

func shouldLog(level LogLevel) bool {
	return level >= LogLevel(atomic.LoadInt32(&logLevel))
}

func parseLogLevel(level string) (LogLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "trace":
		return LevelTrace, true
	case "debug":
		return LevelDebug, true
	case "info":
		return LevelInfo, true
	case "warn", "warning":
		return LevelWarn, true
	case "error":
		return LevelError, true
	default:
		return LevelInfo, false
	}
}
