package menuentry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		want  []string
	}{
		{
			name:  "plain path",
			entry: Entry{ID: "dev.x.app", Name: "App", Comment: "Does things", Exec: "/home/ann/go/bin/app"},
			want: []string{
				"[Desktop Entry]\n", "Type=Application\n", "Name=App\n", "Comment=Does things\n",
				"Exec=/home/ann/go/bin/app\n", "Icon=dev.x.app\n", "Terminal=false\n", "StartupWMClass=dev.x.app\n",
			},
		},
		{
			name:  "path with a space is quoted",
			entry: Entry{ID: "a", Name: "A", Exec: "/home/ann/my apps/app"},
			want:  []string{`Exec="/home/ann/my apps/app"` + "\n"},
		},
		{
			name:  "quote and dollar are escaped for the Exec key, then the backslash for the file",
			entry: Entry{ID: "a", Name: "A", Exec: `/opt/a"b$c/app`},
			want:  []string{`Exec="/opt/a\\"b\\$c/app"` + "\n"},
		},
		{
			name:  "percent is doubled so it is not a field code",
			entry: Entry{ID: "a", Name: "A", Exec: "/opt/100%/app"},
			want:  []string{"Exec=/opt/100%%/app\n"},
		},
		{
			name:  "a line break in a value stays on its line",
			entry: Entry{ID: "a", Name: "A\nExec=evil", Exec: "/bin/a"},
			want:  []string{`Name=A\nExec=evil` + "\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Render(tt.entry)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("Render() lacks %q:\n%s", w, got)
				}
			}
			if n := strings.Count(got, "\nExec="); n != 1 {
				t.Errorf("Render() has %d Exec lines, want 1:\n%s", n, got)
			}
		})
	}
}

func TestDataHome(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "unset", env: "", want: "/home/ann/.local/share"},
		{name: "absolute", env: "/data/ann", want: "/data/ann"},
		{name: "relative is ignored, as the specification says", env: "data", want: "/home/ann/.local/share"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", tt.env)
			if got := DataHome("/home/ann"); got != tt.want {
				t.Errorf("DataHome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstall(t *testing.T) {
	dataHome := t.TempDir()
	entry := Entry{ID: "dev.x.app", Name: "App", Exec: "/bin/app", IconPNG: []byte("png-one")}

	res, err := Install(dataHome, entry)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dataHome, "applications", "dev.x.app.desktop"); res.DesktopFile != want {
		t.Errorf("DesktopFile = %q, want %q", res.DesktopFile, want)
	}
	if want := filepath.Join(dataHome, "icons", "hicolor", "64x64", "apps", "dev.x.app.png"); res.IconFile != want {
		t.Errorf("IconFile = %q, want %q", res.IconFile, want)
	}

	// A second call with a moved program and a new icon replaces both files.
	entry.Exec, entry.IconPNG = "/elsewhere/app", []byte("png-two")
	if _, err := Install(dataHome, entry); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, res.DesktopFile); !strings.Contains(got, "Exec=/elsewhere/app\n") {
		t.Errorf("entry was not replaced:\n%s", got)
	}
	if got := readFile(t, res.IconFile); got != "png-two" {
		t.Errorf("icon = %q, want png-two", got)
	}
	for _, path := range []string{res.DesktopFile, res.IconFile} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != fileMode {
			t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), fileMode)
		}
	}
	for _, dir := range []string{filepath.Dir(res.DesktopFile), filepath.Dir(res.IconFile)} {
		names, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(names) != 1 {
			t.Errorf("%s holds %d files, want only the one written (no temporary files)", dir, len(names))
		}
	}
}

func TestInstallFailure(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, dataHome string)
	}{
		{
			name: "a file where the icon folder should be",
			prepare: func(t *testing.T, dataHome string) {
				t.Helper()
				writeBlocker(t, filepath.Join(dataHome, "icons"))
			},
		},
		{
			name: "a file where the applications folder should be",
			prepare: func(t *testing.T, dataHome string) {
				t.Helper()
				writeBlocker(t, filepath.Join(dataHome, "applications"))
			},
		},
		{
			name: "a folder where the entry should be",
			prepare: func(t *testing.T, dataHome string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(dataHome, "applications", "a.desktop", "x"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataHome := t.TempDir()
			tt.prepare(t, dataHome)
			_, err := Install(dataHome, Entry{ID: "a", Name: "A", Exec: "/bin/a", IconPNG: []byte("x")})
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("Install->writeFile.DesktopEntryWriteFailed") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func writeBlocker(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
