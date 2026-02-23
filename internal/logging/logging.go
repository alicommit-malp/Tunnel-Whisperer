package logging

import (
	"log/slog"
	"os"
	"strings"
)

// XrayLevel holds the Xray-compatible log level string (e.g. "debug", "warning").
// Set by Setup() and read by the xray package when building configs.
var XrayLevel = "warning"

// Setup initializes the default slog logger at the given level.
// Valid levels: "debug", "info", "warn", "error". Defaults to "info".
func Setup(level string) {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
		XrayLevel = "debug"
	case "warn", "warning":
		l = slog.LevelWarn
		XrayLevel = "warning"
	case "error":
		l = slog.LevelError
		XrayLevel = "error"
	default:
		l = slog.LevelInfo
		XrayLevel = "warning"
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})
	slog.SetDefault(slog.New(h))
}
