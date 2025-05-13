package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "🔍 DEBUG"
	case INFO:
		return "ℹ️  INFO"
	case WARN:
		return "⚠️  WARN"
	case ERROR:
		return "❌ ERROR"
	default:
		return "UNKNOWN"
	}
}

var logLevel = INFO

type Logger struct {
	prefix string
}

func NewLogger(prefix string) *Logger {
	return &Logger{prefix: prefix}
}

func (l *Logger) log(level LogLevel, format string, v ...interface{}) {
	if level >= logLevel {
		timestamp := time.Now().Format("2006-01-02 15:04:05.000")
		message := fmt.Sprintf(format, v...)
		prefix := level.String()
		if l.prefix != "" {
			prefix = fmt.Sprintf("%s [%s]", prefix, l.prefix)
		}
		log.Printf("%s %s: %s\n", prefix, timestamp, message)
	}
}

func (l *Logger) Debug(format string, v ...interface{}) { l.log(DEBUG, format, v...) }
func (l *Logger) Info(format string, v ...interface{})  { l.log(INFO, format, v...) }
func (l *Logger) Warn(format string, v ...interface{})  { l.log(WARN, format, v...) }
func (l *Logger) Error(format string, v ...interface{}) { l.log(ERROR, format, v...) }

func setLogLevel(level string) LogLevel {
	var newLevel LogLevel
	switch strings.ToUpper(level) {
	case "DEBUG":
		newLevel = DEBUG
	case "INFO":
		newLevel = INFO
	case "WARN":
		newLevel = WARN
	case "ERROR":
		newLevel = ERROR
	default:
		log.Printf("⚠️  Invalid log level %s, using INFO", level)
		newLevel = INFO
	}
	logLevel = newLevel
	log.Printf("📊 Setting log level to %s", newLevel)
	return newLevel
}