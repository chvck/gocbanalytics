package cbanalytics

import "github.com/couchbaselabs/gocbanalytics/internal/logging"

// LogLevel specifies the severity of a log message.
type LogLevel int

// Various logging levels (or subsystems) which can categorize the message.
// Currently these are ordered in decreasing severity.
const (
	LogError = LogLevel(logging.LogError)
	LogWarn  = LogLevel(logging.LogWarn)
	LogInfo  = LogLevel(logging.LogInfo)
	LogDebug = LogLevel(logging.LogDebug)
	LogTrace = LogLevel(logging.LogTrace)
)

// Logger defines a logging interface.
type Logger interface {
	// Error outputs an error level log message.
	Error(format string, v ...interface{})

	// Warn outputs an warn level log message.
	Warn(format string, v ...interface{})

	// Info outputs an info level log message.
	Info(format string, v ...interface{})

	// Debug outputs a debug level error log message.
	Debug(format string, v ...interface{})

	// Trace outputs a trace level error log message.
	Trace(format string, v ...interface{})
}

type baseLogger struct {
	logger *logging.DefaultLogger
}

func (b baseLogger) Error(format string, v ...interface{}) {
	b.logger.Error(format, v...)
}

func (b baseLogger) Warn(format string, v ...interface{}) {
	b.logger.Warn(format, v...)
}

func (b baseLogger) Info(format string, v ...interface{}) {
	b.logger.Info(format, v...)
}

func (b baseLogger) Debug(format string, v ...interface{}) {
	b.logger.Debug(format, v...)
}

func (b baseLogger) Trace(format string, v ...interface{}) {
	b.logger.Trace(format, v...)
}

// InfoLogger logs to stderr with a level of LogInfo.
type InfoLogger struct {
	baseLogger
}

// NewInfoLogger creates a new InfoLogger instance.
func NewInfoLogger() *InfoLogger {
	return &InfoLogger{
		baseLogger: baseLogger{
			logger: logging.NewDefaultLogger(logging.LogInfo, 0),
		},
	}
}

// VerboseLogger logs to stderr with a level of LogTrace.
type VerboseLogger struct {
	baseLogger
}

// NewVerboseLogger creates a new VerboseLogger instance.
func NewVerboseLogger() *VerboseLogger {
	return &VerboseLogger{
		baseLogger: baseLogger{
			logger: logging.NewDefaultLogger(logging.LogTrace, 7),
		},
	}
}

// NoopLogger is a no-operation logger that ignores all log messages.
// This is equivalent to specifying Logger on ClusterOptions as nil.
type NoopLogger struct {
}

// NewNoopLogger creates a new NoopLogger instance.
func NewNoopLogger() *NoopLogger {
	return &NoopLogger{}
}

func (n NoopLogger) Error(_ string, _ ...interface{}) {
}

func (n NoopLogger) Warn(_ string, _ ...interface{}) {
}

func (n NoopLogger) Info(_ string, _ ...interface{}) {
}

func (n NoopLogger) Debug(_ string, _ ...interface{}) {
}

func (n NoopLogger) Trace(_ string, _ ...interface{}) {
}
