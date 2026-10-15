package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertTag(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with route %q, got nil", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestReadTextFile(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantText   string
		wantEnding string
	}{
		{"plain", []byte("Hello.\nWorld."), "Hello.\nWorld.", "\n"},
		{"empty", nil, "", "\n"},
		{"unicode", []byte("café 😀\nnaïve"), "café 😀\nnaïve", "\n"},
		{"byte order mark is dropped", []byte("\xef\xbb\xbfHello"), "Hello", "\n"},
		{"windows line endings are remembered", []byte("one\r\ntwo\r\n"), "one\ntwo\n", "\r\n"},
		{"mixed endings count as windows", []byte("one\r\ntwo\nthree"), "one\ntwo\nthree", "\r\n"},
		{"old mac line endings", []byte("one\rtwo"), "one\ntwo", "\n"},
		{"no trailing newline is kept", []byte("abc"), "abc", "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, "a.txt", tt.data)
			text, ending, err := readTextFile(context.Background(), path)
			if err != nil || text != tt.wantText || ending != tt.wantEnding {
				t.Errorf("readTextFile() = %q, %q, %v; want %q, %q", text, ending, err, tt.wantText, tt.wantEnding)
			}
		})
	}
}

func TestReadTextFileFailures(t *testing.T) {
	big := writeFile(t, "big.txt", bytes.Repeat([]byte("a"), maxFileBytes+1))
	tests := []struct {
		name      string
		path      string
		route     string
		wantInMsg string
	}{
		{"missing file", filepath.Join(t.TempDir(), "nope.txt"), "readTextFile->fileReadFailed.FileReadFailed", "no such file"},
		{"a folder", t.TempDir(), "readTextFile->fileReadFailed.FileReadFailed", "directory"},
		{"too large", big, "readTextFile.FileReadFailed", "too large"},
		{"invalid utf-8", writeFile(t, "latin1.txt", []byte("caf\xe9")), "readTextFile.FileNotUTF8", "not valid UTF-8"},
		{"binary", writeFile(t, "bin.dat", []byte("abc\x00def")), "readTextFile.FileNotUTF8", "does not look like a text file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := readTextFile(context.Background(), tt.path)
			assertTag(t, err, tt.route)
			if appErr, _ := errors.FromError(err); !strings.Contains(appErr.Message, tt.wantInMsg) {
				t.Errorf("message %q should contain %q", appErr.Message, tt.wantInMsg)
			}
		})
	}
}

func TestReadTextFileCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := readTextFile(ctx, writeFile(t, "a.txt", []byte("x")))
	assertTag(t, err, "readTextFile.FileReadFailed")
}

func TestWriteTextFile(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		ending string
		want   string
	}{
		{"unix", "a\nb", lineEndingLF, "a\nb"},
		{"windows", "a\nb\n", lineEndingCRLF, "a\r\nb\r\n"},
		{"empty", "", lineEndingLF, ""},
		{"unicode", "café 😀", lineEndingLF, "café 😀"},
		{"unknown ending is unix", "a\nb", "", "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.txt")
			if err := writeTextFile(context.Background(), path, tt.text, tt.ending); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != tt.want {
				t.Errorf("file = %q, %v; want %q", got, err, tt.want)
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
				t.Errorf("a new file is 0644, got %v, %v", info.Mode().Perm(), err)
			}
		})
	}
}

func TestWriteTextFileRoundTripKeepsWindowsEndings(t *testing.T) {
	path := writeFile(t, "w.txt", []byte("one\r\ntwo\r\n"))
	text, ending, err := readTextFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTextFile(context.Background(), path, text+"three\n", ending); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "one\r\ntwo\r\nthree\r\n" {
		t.Errorf("file = %q", got)
	}
}

func TestWriteTextFileKeepsPermissionsAndLeavesNoTempFile(t *testing.T) {
	path := writeFile(t, "private.txt", []byte("old"))
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeTextFile(context.Background(), path, "new", lineEndingLF); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v; an existing file keeps its permissions", info.Mode().Perm(), err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Errorf("folder has %d entries (%v), want only the file", len(entries), err)
	}
}

func TestWriteTextFileFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symbolic links are not available: ", err)
	}
	if err := writeTextFile(context.Background(), link, "new", lineEndingLF); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link must stay a link: %v, %v", info, err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Errorf("target = %q", got)
	}
}

func TestWriteTextFileFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	tests := []struct {
		name  string
		ctx   context.Context
		path  string
		route string
		skip  bool
	}{
		{"missing folder", context.Background(), filepath.Join(dir, "nope", "a.txt"), "writeTextFile->fileWriteFailed.FileWriteFailed", false},
		{"read-only folder", context.Background(), filepath.Join(locked, "a.txt"), "writeTextFile->fileWriteFailed.FileWriteFailed", os.Geteuid() == 0},
		{"cancelled", ctx, filepath.Join(dir, "a.txt"), "writeTextFile.FileWriteFailed", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip("permissions do not apply to root")
			}
			err := writeTextFile(tt.ctx, tt.path, "text", lineEndingLF)
			assertTag(t, err, tt.route)
			if appErr, _ := errors.FromError(err); appErr.Message == "" {
				t.Error("the message is shown to the user and must not be empty")
			}
		})
	}
}
