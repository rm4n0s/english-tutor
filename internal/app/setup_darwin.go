//go:build darwin

package app

import (
	"fmt"
	"path/filepath"

	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/menuentry"
)

// installDesktop adds the program at exe to the Launchpad and Spotlight as an application bundle in ~/Applications.
func installDesktop(home, exe string) (string, error) {
	b, err := installBundle(filepath.Join(home, "Applications"), exe)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("English Tutor is now in your Applications folder.\n  app:   %s\n  runs:  %s", b.App, exe), nil
}

// installBundle writes the application bundle of the program at exe under dir.
func installBundle(dir, exe string) (menuentry.Bundle, error) {
	icon, err := gui.IconPNG()
	if err != nil {
		return menuentry.Bundle{}, err
	}
	return menuentry.InstallBundle(dir, entryOf(exe, icon))
}
