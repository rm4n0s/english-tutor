package app

import (
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/menuentry"
)

// setupDesktop is the -setup-desktop flag: it adds English Tutor to the
// application menu (a desktop entry on Linux, an application bundle in
// ~/Applications on macOS), pointing at the program that is running, and returns
// what to tell the user.
func setupDesktop(home string, executable func() (string, error)) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", errors.NewErr("ExecutablePathNotFound", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return installDesktop(home, exe)
}

// entryOf describes English Tutor, run from exe, to the desktop.
func entryOf(exe string, icon []byte) menuentry.Entry {
	return menuentry.Entry{
		ID:      gui.AppID(),
		Name:    "English Tutor",
		Comment: "Practise written English with an LLM that underlines your mistakes",
		Exec:    exe,
		IconPNG: icon,
	}
}
