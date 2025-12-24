package migrate

import "fmt"

// Logger defines the interface for migration logging.
// Implementations can wrap slog, zap, logrus, or any logger.
type Logger interface {
	// Printf logs a formatted message at info level
	Printf(format string, args ...interface{})
	// Debugf logs a formatted message at debug level
	Debugf(format string, args ...interface{})
	// Errorf logs a formatted message at error level
	Errorf(format string, args ...interface{})
}

// DefaultLogger uses fmt.Printf for all logging.
// It implements the Logger interface with basic stdout output.
type DefaultLogger struct {
	// Debug enables debug level logging when true
	Debug bool
}

// NewDefaultLogger creates a new DefaultLogger with optional debug mode.
func NewDefaultLogger(debug bool) *DefaultLogger {
	return &DefaultLogger{Debug: debug}
}

// Printf logs a formatted message at info level.
func (l *DefaultLogger) Printf(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

// Debugf logs a formatted message at debug level.
// Messages are only printed if Debug is enabled.
func (l *DefaultLogger) Debugf(format string, args ...interface{}) {
	if l.Debug {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// Errorf logs a formatted message at error level.
func (l *DefaultLogger) Errorf(format string, args ...interface{}) {
	fmt.Printf("[ERROR] "+format+"\n", args...)
}

// NopLogger is a no-operation logger that discards all messages.
// Useful for testing or when logging should be completely disabled.
type NopLogger struct{}

// Printf does nothing.
func (l *NopLogger) Printf(format string, args ...interface{}) {}

// Debugf does nothing.
func (l *NopLogger) Debugf(format string, args ...interface{}) {}

// Errorf does nothing.
func (l *NopLogger) Errorf(format string, args ...interface{}) {}
