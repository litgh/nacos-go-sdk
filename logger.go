package nacos

import (
	"log"
	"time"

	logrotate "github.com/lestrrat-go/file-rotatelogs"
)

type LogLevel int

const (
	LogOff LogLevel = iota + 1
	LogError
	LogWarn
	LogInfo
	LogDebug
)

var LogTag = []string{"[ERROR]", "[WARN ]", "[INFO ]", "[DEBUG]"}

type Logger interface {
	Error(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Info(format string, args ...interface{})
	Debug(format string, args ...interface{})
	IsDebugEnable() bool
	IsInfoEnable() bool
	IsErrorEnable() bool
	IsWarnEnable() bool
}

func NewLogger(file string, level LogLevel) (Logger, error) {
	writer, err := logrotate.New(
		file+".%Y-%m-%d",
		logrotate.WithLinkName(file),
		logrotate.WithMaxAge(time.Hour*24*30),
		logrotate.WithRotationTime(time.Hour*24),
	)
	if err != nil {
		return nil, err
	}
	log.SetOutput(writer)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	return &logger{level}, nil
}

type logger struct {
	level LogLevel
}

func (l *logger) Error(format string, args ...interface{}) {
	if l.level >= LogError {
		log.Printf(LogTag[LogError-2]+format, args...)
	}
}
func (l *logger) Warn(format string, args ...interface{}) {
	if l.level >= LogWarn {
		log.Printf(LogTag[LogWarn-2]+format, args...)
	}
}
func (l *logger) Info(format string, args ...interface{}) {
	if l.level >= LogInfo {
		log.Printf(LogTag[LogInfo-2]+format, args...)
	}
}
func (l *logger) Debug(format string, args ...interface{}) {
	if l.level >= LogDebug {
		log.Printf(LogTag[LogDebug-2]+format, args...)
	}
}

func (l *logger) IsDebugEnable() bool {
	return l.level&LogDebug == LogDebug
}
func (l *logger) IsErrorEnable() bool {
	return l.level&LogError == LogError
}
func (l *logger) IsInfoEnable() bool {
	return l.level&LogInfo == LogInfo
}
func (l *logger) IsWarnEnable() bool {
	return l.level&LogWarn == LogWarn
}
