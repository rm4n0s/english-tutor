//go:build !linux

package instance

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestSocketPathOfAShortDirectoryIsInIt(t *testing.T) {
	dir := shortDir(t)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // a directory opened for its name
	got, err := socketPath(f)
	if err != nil || got != filepath.Join(dir, socketName) {
		t.Errorf("socketPath = %q, %v; want the file of the directory", got, err)
	}
}

// tmpDir is a folder with a short path, the TMPDIR of a test (a socket address holds about 100 bytes).
func tmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "s")
	if err != nil {
		t.Skipf("no /tmp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func TestSocketPathOfALongDirectoryIsInAPrivateFolder(t *testing.T) {
	tmp := tmpDir(t)
	t.Setenv("TMPDIR", tmp)
	dir := filepath.Join(tmp, strings.Repeat("d", 150))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // a directory opened for its name
	got, err := socketPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > maxSocketPath || !strings.HasPrefix(got, tmp) || !strings.HasSuffix(got, ".sock") {
		t.Errorf("socketPath = %q (%d bytes)", got, len(got))
	}
	if st, err := os.Stat(filepath.Dir(got)); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("the folder: %v, %v", st, err)
	}
	// The same directory gives the same address, and another directory another one.
	again, _ := socketPath(f)
	other := filepath.Join(tmp, strings.Repeat("e", 150))
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	g, err := os.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }() // a directory opened for its name
	third, _ := socketPath(g)
	if again != got || third == got {
		t.Errorf("addresses: %q, %q, %q", got, again, third)
	}
}

func TestSocketPathFailures(t *testing.T) {
	long := filepath.Join(shortDir(t), strings.Repeat("d", 150))
	if err := os.Mkdir(long, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(long)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // a directory opened for its name

	t.Run("the folder cannot be made", func(t *testing.T) {
		file := filepath.Join(shortDir(t), "afile")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TMPDIR", file)
		_, err := socketPath(f)
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "InstanceSocketDirFailed" || !appErr.HasRoute("socketPath.InstanceSocketDirFailed") {
			t.Errorf("error %v, want InstanceSocketDirFailed", err)
		}
	})
	t.Run("the folder is open to others", func(t *testing.T) {
		tmp := tmpDir(t)
		t.Setenv("TMPDIR", tmp)
		folder := filepath.Join(tmp, "english-tutor-"+itoa(os.Getuid()))
		if err := os.Mkdir(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := socketPath(f)
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "InstanceSocketDirUnsafe" || !appErr.HasRoute("checkPrivate.InstanceSocketDirUnsafe") {
			t.Errorf("error %v, want InstanceSocketDirUnsafe", err)
		}
	})
	t.Run("the folder is not the user's", func(t *testing.T) {
		tmp := shortDir(t)
		err := checkPrivate(tmp, syscall.Lstat, os.Getuid()+1)
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "InstanceSocketDirUnsafe" {
			t.Errorf("error %v, want InstanceSocketDirUnsafe", err)
		}
	})
	t.Run("the folder cannot be looked at", func(t *testing.T) {
		err := checkPrivate("/nowhere", func(string, *syscall.Stat_t) error { return syscall.EACCES }, os.Getuid())
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "InstanceSocketDirUnreadable" || !appErr.HasRoute("checkPrivate.InstanceSocketDirUnreadable") {
			t.Errorf("error %v, want InstanceSocketDirUnreadable", err)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
