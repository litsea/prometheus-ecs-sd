package log

import (
	"log/slog"
	"os"
)

type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

func New(logLevel string) *slog.Logger {
	var lvl slog.Level
	err := lvl.UnmarshalText([]byte(logLevel))
	if err != nil {
		lvl = slog.LevelError
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	}))
}

type NopLogger struct{}

func (l *NopLogger) Debug(msg string, args ...any) {}
func (l *NopLogger) Info(msg string, args ...any)  {}
func (l *NopLogger) Warn(msg string, args ...any)  {}
func (l *NopLogger) Error(msg string, args ...any) {}

func NewNopLogger() *NopLogger {
	return &NopLogger{}
}
