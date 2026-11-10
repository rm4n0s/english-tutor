//go:build !darwin

package app

import (
	"fmt"

	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/menuentry"
)

// installDesktop adds the program at exe to the application menu with a freedesktop desktop entry.
func installDesktop(home, exe string) (string, error) {
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
	return menuentry.Install(dataHome, entryOf(exe, icon))
}
