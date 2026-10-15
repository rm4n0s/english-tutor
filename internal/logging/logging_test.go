package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/config"
)

func TestRotation(t *testing.T) {
	tests := []struct {
		name      string
		maxSize   int64
		keep      int
		writes    int
		wantFiles []string // names expected next to the live file
	}{
		{name: "no rotation below limit", maxSize: 100, keep: 3, writes: 2, wantFiles: nil},
		{name: "one rotation", maxSize: 25, keep: 3, writes: 3, wantFiles: []string{".1"}},
		{name: "oldest dropped", maxSize: 12, keep: 2, writes: 6, wantFiles: []string{".1", ".2"}},
		{name: "keep zero drops old logs", maxSize: 12, keep: 0, writes: 4, wantFiles: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "logs", "app.log")
			f, err := OpenRotatingFile(path, tt.maxSize, tt.keep)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}()
			for range tt.writes {
				if _, err := f.Write([]byte("0123456789\n")); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, e := range entries {
				got[e.Name()] = true
			}
			want := map[string]bool{"app.log": true}
			for _, suffix := range tt.wantFiles {
				want["app.log"+suffix] = true
			}
			if len(got) != len(want) {
				t.Fatalf("files = %v, want %v", got, want)
			}
			for name := range want {
				if !got[name] {
					t.Errorf("missing %s; files = %v", name, got)
				}
			}
		})
	}
}

func TestRotationKeepsNewestInLiveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	f, err := OpenRotatingFile(path, 12, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, line := range []string{"first\n", "second\n", "third\n"} {
		if _, err := f.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct{ file, want string }{
		{path, "third\n"},
		{path + ".1", "second\n"},
		{path + ".2", "first\n"},
	}
	for _, tt := range tests {
		b, err := os.ReadFile(tt.file)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tt.want {
			t.Errorf("%s = %q, want %q", filepath.Base(tt.file), b, tt.want)
		}
	}
}

func TestFileModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	path := filepath.Join(dir, "app.log")
	// A pre-existing, too-open file is tightened.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenRotatingFile(path, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != config.FileMode {
		t.Errorf("file mode = %o, want %o", info.Mode().Perm(), config.FileMode)
	}
}

func TestOpenFailure(t *testing.T) {
	// A regular file where the log directory should be.
	blocker := filepath.Join(t.TempDir(), "logs")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenRotatingFile(filepath.Join(blocker, "app.log"), 100, 1)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("OpenRotatingFile.LogFileOpenFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestRotateFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not apply to root")
	}
	dir := filepath.Join(t.TempDir(), "logs")
	path := filepath.Join(dir, "app.log")
	f, err := OpenRotatingFile(path, 12, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := f.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	// A read-only directory makes the rename fail.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(dir, config.DirMode); err != nil {
			t.Error(err)
		}
	}()
	n, err := f.Write([]byte("second\n"))
	if n != len("second\n") {
		t.Errorf("n = %d, want the full line to be written", n)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("RotatingFile.rotate.LogRotateFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestNewFallsBackToStderr(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "logs")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{LogDir: blocker, LogFile: filepath.Join(blocker, "app.log")}
	var stderr bytes.Buffer
	logger, closer := New(paths, &stderr)
	logger.Info("still working")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	out := stderr.String()
	for _, want := range []string{"log file unavailable", "LogFileOpenFailed", "still working"} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr missing %q:\n%s", want, out)
		}
	}
}

func TestNewWritesToFile(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	var stderr bytes.Buffer
	logger, closer := New(paths, &stderr)
	logger.Info("hello", "tokens", 12)
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(paths.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "hello") || !strings.Contains(string(b), "tokens=12") {
		t.Errorf("log file = %q", b)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr output: %q", stderr.String())
	}
}

func TestLogError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "app error is logged as JSON",
			err:  errors.New("SomeTag", "boom", "k", "v"),
			want: []string{`SomeTag`, `boom`, `\"k\":\"v\"`},
		},
		{name: "plain error is logged as text", err: os.ErrNotExist, want: []string{"file does not exist"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			LogError(slog.New(slog.NewTextHandler(&buf, nil)), "failed", tt.err)
			for _, want := range tt.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("output missing %q:\n%s", want, buf.String())
				}
			}
		})
	}
}
