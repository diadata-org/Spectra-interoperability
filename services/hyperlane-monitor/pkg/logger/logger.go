package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

// Fields is an alias for logrus.Fields
type Fields = logrus.Fields

func init() {
	log = logrus.New()
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z",
	})
	
	// Set log level from environment
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	
	level, err := logrus.ParseLevel(strings.ToLower(logLevel))
	if err != nil {
		log.Warnf("Invalid log level %s, using info", logLevel)
		level = logrus.InfoLevel
	}
	log.SetLevel(level)
}

// GetLogger returns the logger instance
func GetLogger() *logrus.Logger {
	return log
}

// WithField creates an entry with a single field
func WithField(key string, value interface{}) *logrus.Entry {
	return log.WithField(key, value)
}

// WithFields creates an entry with multiple fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return log.WithFields(fields)
}

// Info logs at info level
func Info(args ...interface{}) {
	log.Info(args...)
}

// Infof logs at info level with format
func Infof(format string, args ...interface{}) {
	log.Infof(format, args...)
}

// Debug logs at debug level
func Debug(args ...interface{}) {
	log.Debug(args...)
}

// Debugf logs at debug level with format
func Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// Warn logs at warn level
func Warn(args ...interface{}) {
	log.Warn(args...)
}

// Warnf logs at warn level with format
func Warnf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// Error logs at error level
func Error(args ...interface{}) {
	log.Error(args...)
}

// Errorf logs at error level with format
func Errorf(format string, args ...interface{}) {
	log.Errorf(format, args...)
}

// Fatal logs at fatal level and exits
func Fatal(args ...interface{}) {
	log.Fatal(args...)
}

// Fatalf logs at fatal level with format and exits
func Fatalf(format string, args ...interface{}) {
	log.Fatalf(format, args...)
}

// WithError creates an entry with an error field
func WithError(err error) *logrus.Entry {
	return log.WithError(err)
}

// SetLevel sets the log level
func SetLevel(level string) {
	lvl, err := logrus.ParseLevel(strings.ToLower(level))
	if err != nil {
		log.Warnf("Invalid log level %s, using info", level)
		lvl = logrus.InfoLevel
	}
	log.SetLevel(lvl)
}