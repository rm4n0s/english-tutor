package app

import (
	"context"
	"fmt"
	"os"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/english-tutor/internal/config"
	"github.com/rm4n0s/english-tutor/internal/gui"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// Run starts the application and returns its exit code. It prepares
// ~/.english-tutor, opens the log and the database, and runs the window until it
// ends. With a system tray, closing the window hides it and the tray's Close item
// ends the application; without one, closing the window ends it.
func Run(args []string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	home, err := config.HomeDir()
	if err != nil {
		return fatal(err)
	}
	paths := config.NewPaths(home)
	if err := paths.Ensure(); err != nil {
		return fatal(err)
	}
	log, closeLog := logging.New(paths, os.Stderr)
	defer func() {
		if err := closeLog.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "english-tutor: closing log:", err)
		}
	}()

	st, err := store.Open(ctx, paths.DBFile)
	if err != nil {
		logging.LogError(log, "opening database", err)
		return fatal(err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logging.LogError(log, "closing database", err)
		}
	}()

	log.Info("english-tutor starting", "data_dir", paths.DataDir)
	a := New(ctx, log, st)
	a.Show()
	a.EnableTray()
	width, height := a.win.WindowSize()
	code := 0
	err = widgets.RunApp(a.Root(), widgets.RunAppOpts{
		AppID:  gui.AppID(),
		Window: widgets.WindowConfig{Title: "English Tutor", Size: ui.Size{Width: float64(width), Height: float64(height)}},
		// glutter reports that the desktop has no tray icon as an error; the
		// application then closes with its window, as it does without a tray.
		ErrorReporter: func(err error) {
			if gui.IsTrayUnavailable(err) {
				a.TrayUnavailable()
				return
			}
			logging.LogError(log, "framework", err)
		},
	})
	if err != nil {
		logging.LogError(log, "running the window", err)
		fmt.Fprintln(os.Stderr, "english-tutor:", userMessage(err))
		code = 1
	}
	cancel()
	a.web.Stop() // the pages in browsers end before the database closes
	a.Wait()     // a check or a save that was still running ends before the database closes
	log.Info("english-tutor stopped", "code", code)
	return code
}

// fatal tells the user why the application cannot start and returns the exit
// code. The message of an application error is written for the user; anything
// else is shown as it is.
func fatal(err error) int {
	msg := err.Error()
	if appErr, ok := errors.FromError(err); ok {
		msg = appErr.Message
	}
	fmt.Fprintln(os.Stderr, "english-tutor: English Tutor cannot start.\n\n"+msg)
	return 1
}
