package logging

import (
	"encoding/json"
	"io"
	"log/slog"

	"github.com/rm4n0s/errors"

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

// LogError logs err at error level. Errors from github.com/rm4n0s/errors are
// logged as their JSON form so they can be replayed in tests.
func LogError(logger *slog.Logger, msg string, err error) {
	if appErr, ok := errors.FromError(err); ok {
		if j, jerr := appErr.ToJson(); jerr == nil {
			if b, merr := json.Marshal(j); merr == nil {
				logger.Error(msg, "error", string(b))
				return
			}
		}
	}
	logger.Error(msg, "error", err.Error())
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
