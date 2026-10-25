package app

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestSetupDesktop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	exe := filepath.Join(t.TempDir(), "english-tutor")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link-to-english-tutor")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}

	summary, err := setupDesktop(home, func() (string, error) { return link, nil })
	if err != nil {
		t.Fatal(err)
	}

	desktopFile := filepath.Join(home, ".local", "share", "applications", "dev.rm4n0s.english-tutor.desktop")
	data, err := os.ReadFile(desktopFile)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Name=English Tutor\n", "Exec=" + resolved + "\n", "Icon=dev.rm4n0s.english-tutor\n", "StartupWMClass=dev.rm4n0s.english-tutor\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("desktop entry lacks %q:\n%s", want, data)
		}
	}
	if !strings.Contains(summary, desktopFile) || !strings.Contains(summary, resolved) {
		t.Errorf("summary does not name the entry and the program:\n%s", summary)
	}

	icon, err := os.ReadFile(filepath.Join(home, ".local", "share", "icons", "hicolor", "64x64", "apps", "dev.rm4n0s.english-tutor.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		t.Fatalf("the icon is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Errorf("icon is %dx%d, want 64x64", b.Dx(), b.Dy())
	}
}

func TestSetupDesktopFailure(t *testing.T) {
	tests := []struct {
		name       string
		home       func(t *testing.T) string
		executable func() (string, error)
		wantRoute  string
	}{
		{
			name:       "the program cannot be found",
			home:       func(t *testing.T) string { t.Helper(); return t.TempDir() },
			executable: func() (string, error) { return "", os.ErrNotExist },
			wantRoute:  "setupDesktop.ExecutablePathNotFound",
		},
		{
			name: "the menu folders cannot be made",
			home: func(t *testing.T) string {
				t.Helper()
				home := t.TempDir()
				if err := os.WriteFile(filepath.Join(home, ".local"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return home
			},
			executable: func() (string, error) { return "/bin/english-tutor", nil },
			wantRoute:  "setupDesktop->installMenuEntry->Install->writeFile.DesktopEntryWriteFailed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", "")
			_, err := setupDesktop(tt.home(t), tt.executable)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tt.wantRoute) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
