package agent

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// LogFormat selects how agent logs are rendered.
type LogFormat string

const (
	// FormatJSON is one JSON object per line, for a log collector.
	FormatJSON LogFormat = "json"
	// FormatText is key=value, for reading in a terminal.
	FormatText LogFormat = "text"
)

// NewLogger builds a logger for the agent.
//
// JSON is the default because the project's observability plan is structured
// logs, and because systemd's journal and every log collector can parse them.
func NewLogger(w io.Writer, format LogFormat, level string) (*slog.Logger, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: lvl}

	switch LogFormat(strings.ToLower(string(format))) {
	case "", FormatJSON:
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	case FormatText:
		return slog.New(slog.NewTextHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("%q is not a log format (use %q or %q)", format, FormatJSON, FormatText)
	}
}

// ParseLevel converts a level name to a slog level.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("%q is not a log level (use debug, info, warn or error)", name)
	}
}
