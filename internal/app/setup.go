package app

import (
	"fmt"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/menuentry"
)

// setupDesktop is the -setup-desktop flag: it adds English Tutor to the
// application menu, pointing at the program that is running, and returns what
// to tell the user.
func setupDesktop(home string, executable func() (string, error)) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", errors.NewErr("ExecutablePathNotFound", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	res, err := installMenuEntry(menuentry.DataHome(home), exe)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("English Tutor is now in the application menu.\n  entry: %s\n  icon:  %s\n  runs:  %s", res.DesktopFile, res.IconFile, exe), nil
}

// installMenuEntry writes the menu entry of the program at exe under dataHome.
func installMenuEntry(dataHome, exe string) (menuentry.Result, error) {
	icon, err := gui.IconPNG()
	if err != nil {
		return menuentry.Result{}, err
	}
	return menuentry.Install(dataHome, menuentry.Entry{
		ID:      gui.AppID(),
		Name:    "English Tutor",
		Comment: "Practise written English with an LLM that underlines your mistakes",
		Exec:    exe,
		IconPNG: icon,
	})
}
