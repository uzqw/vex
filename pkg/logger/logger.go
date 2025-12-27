// Copyright 2025 uzqw
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logger

import (
	"context"
	"log/slog"
	"os"
)

// Logger wraps slog.Logger with additional functionality
// Supports both Text and JSON output formats for different environments
type Logger struct {
	*slog.Logger
}

// Format represents the log output format
type Format string

const (
	// FormatText outputs logs in human-readable text format
	FormatText Format = "text"
	// FormatJSON outputs logs in structured JSON format (better for production)
	FormatJSON Format = "json"
)

// Config holds logger configuration
type Config struct {
	Format Format
	Level  slog.Level
}

// New creates a new Logger instance with the specified configuration
// Default: Text format with Info level
func New(cfg Config) *Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: cfg.Level,
	}

	switch cfg.Format {
	case FormatJSON:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
	}
}

// Default creates a logger with default settings (Text format, Info level)
func Default() *Logger {
	return New(Config{
		Format: FormatText,
		Level:  slog.LevelInfo,
	})
}

// WithRequestID adds a request ID to the logger context
// This is crucial for tracing requests across the system
func (l *Logger) WithRequestID(ctx context.Context, requestID string) *Logger {
	return &Logger{
		Logger: l.With(slog.String("request_id", requestID)),
	}
}

// WithFields adds additional fields to the logger
func (l *Logger) WithFields(fields map[string]any) *Logger {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, slog.Any(k, v))
	}
	return &Logger{
		Logger: l.With(args...),
	}
}
