//go:build darwin

package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestSetupDesktopMakesTheBundle(t *testing.T) {
	home := t.TempDir()
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
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(home, "Applications", "English Tutor.app")
	if !strings.Contains(summary, app) || !strings.Contains(summary, resolved) {
		t.Errorf("summary does not name the bundle and the program:\n%s", summary)
	}
	script, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "dev.rm4n0s.english-tutor"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "exec '"+resolved+"'") {
		t.Errorf("the launcher does not run the program:\n%s", script)
	}
	icon, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "dev.rm4n0s.english-tutor.icns"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(icon, []byte("icns")) {
		t.Errorf("the icon is not an icns file")
	}
	// A second run replaces the bundle, as it does after the program moved.
	if _, err := setupDesktop(home, func() (string, error) { return exe, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestSetupDesktopBundleFailure(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "Applications"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := setupDesktop(home, func() (string, error) { return "/bin/english-tutor", nil })
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("setupDesktop->installDesktop->installBundle->InstallBundle.BundleDirFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
