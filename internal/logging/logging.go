package logging

import (
	"io"
	"log/slog"

	"github.com/rm4n0s/english-tutor/internal/config"
)

const (
	// MaxLogSize is the size at which the log file is rotated.
	MaxLogSize = 5 << 20
	// KeepLogs is the number of rotated log files retained.
	KeepLogs = 3
)

// New returns a logger that writes text records to the log file in paths. If
// the file cannot be opened it logs to stderr instead, records the failure as a
// warning there, and carries on. The returned closer is never nil.
func New(paths config.Paths, stderr io.Writer) (*slog.Logger, io.Closer) {
	file, err := OpenRotatingFile(paths.LogFile, MaxLogSize, KeepLogs)
	if err != nil {
		logger := slog.New(slog.NewTextHandler(stderr, nil))
		LogError(logger, "log file unavailable, logging to stderr", err)
		return logger, nopCloser{}
	}
	return slog.New(slog.NewTextHandler(file, nil)), file
}

// LogError logs err at error level. Errors from github.com/rm4n0s/errors
// implement slog.LogValuer, so their tag, route, metadata and stack frames are
// logged as a group that can be replayed in tests via errors.ErrorJson.
func LogError(logger *slog.Logger, msg string, err error) {
	logger.Error(msg, "err", err)
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
