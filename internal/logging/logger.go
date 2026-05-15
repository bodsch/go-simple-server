// Package logging builds the application's structured logger.
package logging

import (
	"log/slog"
	"os"
	"time"
)

// New returns a JSON slog logger writing to stdout, configured to emit
// the timestamp in RFC3339 (no fractional seconds) for consistency with
// the JSON responses returned by the HTTP service.
func New(level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time().UTC()
				a.Value = slog.StringValue(t.Format(time.RFC3339))
			}
			return a
		},
	})
	return slog.New(h)
}
