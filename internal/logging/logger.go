package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level defines severity levels for log events.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARNING"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses level string into a Level.
func ParseLevel(s string) (Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return LevelDebug, nil
	case "INFO":
		return LevelInfo, nil
	case "WARN", "WARNING":
		return LevelWarn, nil
	case "ERROR":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %q", s)
	}
}

// Logger provides thread-safe, leveled, stderr-directed logging with single-line TTY progress updates.
type Logger struct {
	mu                sync.Mutex
	out               io.Writer
	level             Level
	isTerminal        bool
	lastSingleLineLen int
	timeNow           func() time.Time
}

// Option configures Logger settings.
type Option func(*Logger)

// WithLevel configures the minimum log level.
func WithLevel(l Level) Option {
	return func(logger *Logger) {
		logger.level = l
	}
}

// WithOutput configures the output writer and auto-detects TTY status.
func WithOutput(w io.Writer) Option {
	return func(logger *Logger) {
		logger.out = w
		logger.isTerminal = isTerminalWriter(w)
	}
}

// WithTerminal explicitly overrides the TTY terminal state.
func WithTerminal(isTerm bool) Option {
	return func(logger *Logger) {
		logger.isTerminal = isTerm
	}
}

// WithTimeFunc supplies a custom clock for testing.
func WithTimeFunc(fn func() time.Time) Option {
	return func(logger *Logger) {
		logger.timeNow = fn
	}
}

// New constructs a new Logger instance. Defaults to os.Stderr and LevelInfo.
func New(opts ...Option) *Logger {
	l := &Logger{
		out:     os.Stderr,
		level:   LevelInfo,
		timeNow: time.Now,
	}
	l.isTerminal = isTerminalWriter(l.out)
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func (l *Logger) IsTerminal() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.isTerminal
}

func (l *Logger) SetTerminal(isTerm bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.isTerminal = isTerm
}

func (l *Logger) Level() Level {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = w
	l.isTerminal = isTerminalWriter(w)
}

func (l *Logger) formatRecord(level Level, msg string) string {
	ts := l.timeNow().Format("2006-01-02 15:04:05")
	return fmt.Sprintf("%s - %s - %s", ts, level.String(), msg)
}

// Emit writes the record under mutex lock.
func (l *Logger) Emit(level Level, singleLine bool, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	formatted := l.formatRecord(level, msg)

	if singleLine && l.isTerminal {
		if l.lastSingleLineLen > len(formatted) {
			fmt.Fprintf(l.out, "\r%s\r", strings.Repeat(" ", l.lastSingleLineLen))
		}
		fmt.Fprintf(l.out, "\r%s", formatted)
		l.lastSingleLineLen = len(formatted)
	} else {
		if l.lastSingleLineLen > 0 {
			if l.isTerminal {
				fmt.Fprintf(l.out, "\r%s\r", strings.Repeat(" ", l.lastSingleLineLen))
			} else {
				fmt.Fprintln(l.out)
			}
			l.lastSingleLineLen = 0
		}
		fmt.Fprintln(l.out, formatted)
	}
}

func (l *Logger) Debug(msg string) { l.Emit(LevelDebug, false, msg) }
func (l *Logger) Debugf(format string, a ...any) {
	l.Emit(LevelDebug, false, fmt.Sprintf(format, a...))
}
func (l *Logger) Info(msg string)               { l.Emit(LevelInfo, false, msg) }
func (l *Logger) Infof(format string, a ...any) { l.Emit(LevelInfo, false, fmt.Sprintf(format, a...)) }
func (l *Logger) Warn(msg string)               { l.Emit(LevelWarn, false, msg) }
func (l *Logger) Warnf(format string, a ...any) { l.Emit(LevelWarn, false, fmt.Sprintf(format, a...)) }
func (l *Logger) Error(msg string)              { l.Emit(LevelError, false, msg) }
func (l *Logger) Errorf(format string, a ...any) {
	l.Emit(LevelError, false, fmt.Sprintf(format, a...))
}

// SingleLine logs an in-place progress update.
func (l *Logger) SingleLine(msg string) { l.Emit(LevelInfo, true, msg) }
func (l *Logger) SingleLinef(format string, a ...any) {
	l.Emit(LevelInfo, true, fmt.Sprintf(format, a...))
}

// Global default logger instance.
var (
	defaultLoggerMu sync.RWMutex
	defaultLogger   = New()
)

func Default() *Logger {
	defaultLoggerMu.RLock()
	defer defaultLoggerMu.RUnlock()
	return defaultLogger
}

func SetDefault(l *Logger) {
	defaultLoggerMu.Lock()
	defer defaultLoggerMu.Unlock()
	defaultLogger = l
}

func Setup(debug bool) {
	defaultLoggerMu.Lock()
	defer defaultLoggerMu.Unlock()
	level := LevelInfo
	if debug {
		level = LevelDebug
	}
	defaultLogger = New(WithOutput(os.Stderr), WithLevel(level))
}

func SetDebug(debug bool) {
	Setup(debug)
}

func SetOutput(w io.Writer) { Default().SetOutput(w) }
func SetLevel(l Level)      { Default().SetLevel(l) }

func Debug(msg string)               { Default().Debug(msg) }
func Debugf(format string, a ...any) { Default().Debugf(format, a...) }
func Info(msg string)                { Default().Info(msg) }
func Infof(format string, a ...any)  { Default().Infof(format, a...) }
func Warn(msg string)                { Default().Warn(msg) }
func Warnf(format string, a ...any)  { Default().Warnf(format, a...) }
func Error(msg string)               { Default().Error(msg) }
func Errorf(format string, a ...any) { Default().Errorf(format, a...) }
func SingleLine(msg string)          { Default().SingleLine(msg) }
func SingleLinef(format string, a ...any) {
	Default().SingleLinef(format, a...)
}
