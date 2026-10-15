package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestNewPaths(t *testing.T) {
	tests := []struct {
		name string
		home string
		want Paths
	}{
		{
			name: "plain home",
			home: "/home/ann",
			want: Paths{
				DataDir: "/home/ann/.english-tutor",
				DBFile:  "/home/ann/.english-tutor/english-tutor.db",
				LogDir:  "/home/ann/.english-tutor/logs",
				LogFile: "/home/ann/.english-tutor/logs/english-tutor.log",
			},
		},
		{
			name: "home with trailing slash",
			home: "/home/ann/",
			want: Paths{
				DataDir: "/home/ann/.english-tutor",
				DBFile:  "/home/ann/.english-tutor/english-tutor.db",
				LogDir:  "/home/ann/.english-tutor/logs",
				LogFile: "/home/ann/.english-tutor/logs/english-tutor.log",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewPaths(tt.home); got != tt.want {
				t.Errorf("NewPaths(%q) = %+v, want %+v", tt.home, got, tt.want)
			}
		})
	}
}

func TestEnsure(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, home string)
	}{
		{name: "creates missing directory", prepare: func(*testing.T, string) {}},
		{
			name: "tightens permissions of existing directory",
			prepare: func(t *testing.T, home string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(home, ".english-tutor"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			tt.prepare(t, home)
			p := NewPaths(home)
			if err := p.Ensure(); err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}
			info, err := os.Stat(p.DataDir)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != DirMode {
				t.Errorf("mode = %o, want %o", got, DirMode)
			}
		})
	}
}

func TestEnsureFailure(t *testing.T) {
	home := t.TempDir()
	// A file where the data directory should be makes MkdirAll fail.
	blocker := filepath.Join(home, ".english-tutor")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewPaths(home).Ensure()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Paths.Ensure.DataDirCreateFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestHomeDir(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		wantErr bool
	}{
		{name: "home set", env: "/home/ann"},
		{name: "home unset", env: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.env)
			got, err := HomeDir()
			if !tt.wantErr {
				if err != nil || got != tt.env {
					t.Fatalf("HomeDir() = %q, %v; want %q, nil", got, err, tt.env)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("HomeDir.HomeDirNotFound") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
