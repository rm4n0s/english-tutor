// Package menuentry puts English Tutor in the application menu of a Linux desktop
// by writing a freedesktop desktop entry and its icon under the user's data
// directory (XDG_DATA_HOME, or ~/.local/share).
package menuentry

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

const (
	appDir  = "applications"
	iconDir = "icons/hicolor/64x64/apps"

	dirMode  os.FileMode = 0o755
	fileMode os.FileMode = 0o644
)

// Entry describes the application to the desktop.
type Entry struct {
	// ID names the entry: it is the file name without its extension, the icon
	// name and the StartupWMClass, so it should be the window's application ID.
	ID      string
	Name    string
	Comment string
	// Exec is the absolute path of the program.
	Exec string
	// IconPNG is the icon, a 64x64 PNG.
	IconPNG []byte
}

// Result tells where the files were written.
type Result struct {
	DesktopFile string
	IconFile    string
}

// DataHome returns the user's data directory: XDG_DATA_HOME when it is an
// absolute path, otherwise home/.local/share.
func DataHome(home string) string {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(home, ".local", "share")
}

// Install writes the icon and the desktop entry under dataHome, replacing the
// ones of an earlier call, so running it again after the program moved fixes the
// entry. The icon goes first: the entry appears in the menu as soon as it
// exists.
func Install(dataHome string, e Entry) (Result, error) {
	res := Result{
		DesktopFile: filepath.Join(dataHome, appDir, e.ID+".desktop"),
		IconFile:    filepath.Join(dataHome, iconDir, e.ID+".png"),
	}
	if err := writeFile(res.IconFile, e.IconPNG); err != nil {
		return Result{}, err
	}
	if err := writeFile(res.DesktopFile, []byte(Render(e))); err != nil {
		return Result{}, err
	}
	return res, nil
}

// Render returns the text of the desktop entry.
func Render(e Entry) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=" + escapeValue(e.Name) + "\n")
	b.WriteString("Comment=" + escapeValue(e.Comment) + "\n")
	b.WriteString("Exec=" + escapeValue(execQuote(e.Exec)) + "\n")
	b.WriteString("Icon=" + escapeValue(e.ID) + "\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("Categories=Education;Languages;\n")
	b.WriteString("StartupWMClass=" + escapeValue(e.ID) + "\n")
	return b.String()
}

// execQuote makes a program path safe for the Exec key: a path with a character
// the specification reserves is put in double quotes, with ", `, $ and \ escaped,
// and a % is doubled so it is not read as a field code.
func execQuote(path string) string {
	path = strings.ReplaceAll(path, "%", "%%")
	if !strings.ContainsAny(path, " \t\n\"'\\><~|&;$*?#()`") {
		return path
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range path {
		if strings.ContainsRune("\"`$\\", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// escapeValue escapes a string for the value of a key: \ is doubled and a line
// break is written as \n, so a value stays on its line.
func escapeValue(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(s)
}

// writeFile writes data to path through a temporary file renamed over it, so a
// desktop that watches the folder never reads half a file.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return errors.NewErr("DesktopEntryWriteFailed", err).SetMetadata("path", path)
	}
	tmp, err := os.CreateTemp(dir, ".english-tutor-*")
	if err != nil {
		return errors.NewErr("DesktopEntryWriteFailed", err).SetMetadata("path", path)
	}
	if err := fillAndRename(tmp, data, path); err != nil {
		appErr := errors.NewErr("DesktopEntryWriteFailed", err).SetMetadata("path", path)
		if rerr := os.Remove(tmp.Name()); rerr != nil && !os.IsNotExist(rerr) {
			appErr = appErr.SetMetadata("cleanup", rerr.Error())
		}
		return appErr
	}
	return nil
}

// fillAndRename writes data to the open temporary file, closes it and moves it
// to path.
func fillAndRename(tmp *os.File, data []byte, path string) error {
	if _, err := tmp.Write(data); err != nil {
		if cerr := tmp.Close(); cerr != nil {
			return cerr
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), fileMode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
