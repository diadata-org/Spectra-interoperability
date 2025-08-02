package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

func init() {
	log = logrus.New()
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z",
	})
}

// Init initializes the logger with the specified log level
func Init(level string) error {
	logLevel, err := logrus.ParseLevel(strings.ToLower(level))
	if err != nil {
		return err
	}
	log.SetLevel(logLevel)
	return nil
}

// GetLogger returns the configured logger instance
func GetLogger() *logrus.Logger {
	return log
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	log.Debug(args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	log.Debugf(format, args...)
}

// Info logs an info message
func Info(args ...interface{}) {
	log.Info(args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	log.Infof(format, args...)
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	log.Warn(args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	log.Warnf(format, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	log.Error(args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	log.Errorf(format, args...)
}

// Fatal logs a fatal message and exits
func Fatal(args ...interface{}) {
	log.Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(format string, args ...interface{}) {
	log.Fatalf(format, args...)
}

// WithField returns a logger with a field
func WithField(key string, value interface{}) *logrus.Entry {
	return log.WithField(key, value)
}

// WithFields returns a logger with fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return log.WithFields(fields)
}

// WithError returns a logger with an error field
func WithError(err error) *logrus.Entry {
	return log.WithError(err)
}