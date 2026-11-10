package menuentry

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func bundleEntry() Entry {
	return Entry{ID: "dev.x.app", Name: "My: App/1", Comment: "Does <things> & more", Exec: "/home/ann/my apps/it's", IconPNG: []byte("\x89PNGdata")}
}

func TestInstallBundle(t *testing.T) {
	dir := t.TempDir()
	b, err := InstallBundle(dir, bundleEntry())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "My App1.app"); b.App != want {
		t.Errorf("bundle at %s, want %s", b.App, want)
	}
	script, err := os.ReadFile(b.Script)
	if err != nil {
		t.Fatal(err)
	}
	if want := "#!/bin/sh\nexec '/home/ann/my apps/it'\\''s' \"$@\"\n"; string(script) != want {
		t.Errorf("launcher = %q, want %q", script, want)
	}
	if st, err := os.Stat(b.Script); err != nil || st.Mode().Perm() != 0o755 {
		t.Errorf("launcher mode = %v (%v), want 0755", st, err)
	}
	plist, err := os.ReadFile(b.Plist)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<key>CFBundleIdentifier</key>\n\t<string>dev.x.app</string>", "<key>CFBundleExecutable</key>\n\t<string>dev.x.app</string>", "Does &lt;things&gt; &amp; more", "<key>CFBundleName</key>\n\t<string>My: App/1</string>"} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist lacks %q:\n%s", want, plist)
		}
	}
	icon, err := os.ReadFile(b.Icon)
	if err != nil {
		t.Fatal(err)
	}
	png := bundleEntry().IconPNG
	if len(icon) != 16+len(png) || string(icon[:4]) != "icns" || string(icon[8:12]) != "icp6" ||
		binary.BigEndian.Uint32(icon[4:8]) != uint32(len(icon)) || binary.BigEndian.Uint32(icon[12:16]) != uint32(8+len(png)) ||
		!bytes.Equal(icon[16:], png) {
		t.Errorf("icns = %q", icon)
	}
	// Nothing but the bundle is left in the folder, and a second call replaces it.
	if _, err := InstallBundle(dir, bundleEntry()); err != nil {
		t.Fatal(err)
	}
	left, err := os.ReadDir(dir)
	if err != nil || len(left) != 1 {
		t.Errorf("folder holds %v (%v), want only the bundle", left, err)
	}
}

func TestInstallBundleFailure(t *testing.T) {
	tests := []struct {
		name      string
		dir       func(t *testing.T) string
		wantRoute string
	}{
		{
			name: "the folder cannot be made",
			dir: func(t *testing.T) string {
				t.Helper()
				file := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(file, "Applications")
			},
			wantRoute: "InstallBundle.BundleDirFailed",
		},
		{
			name: "the folder cannot be written",
			dir: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				if err := os.Chmod(dir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
				return dir
			},
			wantRoute: "InstallBundle.BundleDirFailed",
		},
		{
			name: "the earlier bundle cannot be replaced",
			dir: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				old := filepath.Join(dir, "My App1.app", "keep")
				if err := os.MkdirAll(old, 0o755); err != nil {
					t.Fatal(err)
				}
				// A read-only folder inside the old bundle cannot be emptied.
				if err := os.WriteFile(filepath.Join(old, "x"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(old, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(old, 0o700) })
				return dir
			},
			wantRoute: "InstallBundle.BundleReplaceFailed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Geteuid() == 0 && strings.Contains(tt.name, "cannot be") && tt.name != "the folder cannot be made" {
				t.Skip("root ignores the permissions")
			}
			_, err := InstallBundle(tt.dir(t), bundleEntry())
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.wantRoute) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
