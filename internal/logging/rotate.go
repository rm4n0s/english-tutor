// Package logging sets up slog with a size-rotated log file.
package logging

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/config"
)

// RotatingFile is an io.WriteCloser that appends to a file and rotates it when
// it would grow past maxSize. Old files are named path.1 (newest) to path.N.
type RotatingFile struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	keep    int
	file    *os.File
	size    int64
}

// OpenRotatingFile opens (creating it and its directory if needed) the log file
// at path with mode 0600. maxSize is in bytes; keep is the number of rotated
// files to retain.
func OpenRotatingFile(path string, maxSize int64, keep int) (*RotatingFile, error) {
	r := &RotatingFile{path: path, maxSize: maxSize, keep: keep}
	if err := os.MkdirAll(filepath.Dir(path), config.DirMode); err != nil {
		return nil, errors.NewErr("LogFileOpenFailed", err).SetMetadata("path", path)
	}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, config.FileMode)
	if err != nil {
		return errors.NewErr("LogFileOpenFailed", err).SetMetadata("path", r.path)
	}
	if err := f.Chmod(config.FileMode); err != nil {
		_ = f.Close()
		return errors.NewErr("LogFileOpenFailed", err).SetMetadata("path", r.path)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return errors.NewErr("LogFileOpenFailed", err).SetMetadata("path", r.path)
	}
	r.file = f
	r.size = info.Size()
	return nil
}

// Write appends p, rotating first if p would not fit. If rotation fails, p is
// still written to the current file and the rotation error is returned.
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var rotateErr error
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		rotateErr = r.rotate()
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	if err != nil {
		return n, err
	}
	return n, rotateErr
}

// Close closes the underlying file.
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

func (r *RotatingFile) rotate() error {
	if r.keep > 0 {
		oldest := r.numbered(r.keep)
		if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
			return errors.NewErr("LogRotateFailed", err).SetMetadata("path", oldest)
		}
		for i := r.keep - 1; i >= 1; i-- {
			from, to := r.numbered(i), r.numbered(i+1)
			if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
				return errors.NewErr("LogRotateFailed", err).SetMetadata("path", from)
			}
		}
		if err := os.Rename(r.path, r.numbered(1)); err != nil {
			return errors.NewErr("LogRotateFailed", err).SetMetadata("path", r.path)
		}
	} else if err := os.Remove(r.path); err != nil {
		return errors.NewErr("LogRotateFailed", err).SetMetadata("path", r.path)
	}

	old := r.file
	if err := r.open(); err != nil {
		// r.file still points at the renamed file, so later writes are not lost.
		return errors.NewErr("LogRotateFailed", err).SetMetadata("path", r.path)
	}
	if err := old.Close(); err != nil {
		return errors.NewErr("LogRotateFailed", err).SetMetadata("path", r.path)
	}
	return nil
}

func (r *RotatingFile) numbered(n int) string {
	return r.path + "." + strconv.Itoa(n)
}
