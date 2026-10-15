package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// maxFileBytes is the largest text file that is opened. A text longer than this
// is not an essay, and reading it would freeze the editor.
const maxFileBytes = 10 << 20

// lineEndingLF and lineEndingCRLF are the two line endings a file can use. The
// editor always works with "\n"; the ending of a file that is opened is
// remembered and written back when it is saved.
const (
	lineEndingLF   = "\n"
	lineEndingCRLF = "\r\n"
)

// readTextFile reads a UTF-8 text file. It returns the text with "\n" line
// endings, whatever the file used, and the line ending to write back. A leading
// byte order mark is dropped.
//
// Errors: FileReadFailed (cannot be read, or too large), FileNotUTF8 (not valid
// UTF-8, or binary). Their messages are written for the user.
func readTextFile(ctx context.Context, path string) (text, lineEnding string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", errors.NewErr("FileReadFailed", err).SetMetadata("path", path).
			SetMessage("The file was not read because the application is closing.")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fileReadFailed(path, err)
	}
	if info.Size() > maxFileBytes {
		return "", "", errors.New("FileReadFailed",
			"The file is too large to open as text (the limit is 10 MB).", "path", path, "size", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fileReadFailed(path, err)
	}
	data = []byte(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return "", "", errors.New("FileNotUTF8",
			"This file is not valid UTF-8 text, so it was not opened.", "path", path)
	}
	if strings.ContainsRune(string(data), 0) {
		return "", "", errors.New("FileNotUTF8",
			"This file does not look like a text file, so it was not opened.", "path", path)
	}
	text = string(data)
	lineEnding = lineEndingLF
	if strings.Contains(text, "\r\n") {
		lineEnding = lineEndingCRLF
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, lineEnding, nil
}

func fileReadFailed(path string, err error) error {
	return errors.NewErr("FileReadFailed", err).SetMetadata("path", path).
		SetMessage("The file could not be read: " + reason(err))
}

// writeTextFile saves text to path. The text is written to a temporary file in
// the same folder and renamed over the target, so a failure half way never
// leaves a damaged file behind. The permissions of an existing file are kept,
// and a symbolic link is followed so that the link stays a link.
//
// Error: FileWriteFailed.
func writeTextFile(ctx context.Context, path, text, lineEnding string) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("FileWriteFailed", err).SetMetadata("path", path).
			SetMessage("The file was not saved because the application is closing.")
	}
	if lineEnding == lineEndingCRLF {
		text = strings.ReplaceAll(text, "\n", lineEndingCRLF)
	}
	target := path
	mode := os.FileMode(0o644)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".english-tutor-*")
	if err != nil {
		return fileWriteFailed(path, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		cleanup()
		return fileWriteFailed(path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fileWriteFailed(path, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fileWriteFailed(path, err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return fileWriteFailed(path, err)
	}
	return nil
}

func fileWriteFailed(path string, err error) error {
	return errors.NewErr("FileWriteFailed", err).SetMetadata("path", path).
		SetMessage("The file could not be saved: " + reason(err))
}

// reason is the operating system's explanation of a file error without the
// path, which the dialog already shows.
func reason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
