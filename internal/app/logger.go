package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// appLogger is the package-level structured logger.
// It defaults to text output on stdout; set JUKU_LOG_FORMAT=json for JSON output.
var appLogger = newLogger()

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if v := os.Getenv("JUKU_LOG_LEVEL"); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			level = slog.LevelDebug
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.EqualFold(os.Getenv("JUKU_LOG_FORMAT"), "json") {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// logInfo logs an informational message with optional key-value pairs.
func logInfo(msg string, args ...any) { appLogger.Info(msg, args...) }

// logWarn logs a warning message with optional key-value pairs.
func logWarn(msg string, args ...any) { appLogger.Warn(msg, args...) }

// logError logs an error message with optional key-value pairs.
func logError(msg string, args ...any) { appLogger.Error(msg, args...) }

// logDebug logs a debug message with optional key-value pairs.
func logDebug(msg string, args ...any) { appLogger.Debug(msg, args...) }

// logInfoCtx logs an informational message with context (for future trace propagation).
func logInfoCtx(ctx context.Context, msg string, args ...any) {
	appLogger.InfoContext(ctx, msg, args...)
}

// logErrorCtx logs an error message with context.
func logErrorCtx(ctx context.Context, msg string, args ...any) {
	appLogger.ErrorContext(ctx, msg, args...)
}
