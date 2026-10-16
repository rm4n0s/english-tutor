package app

import (
	"context"
	"fmt"
	"os"

	qt "github.com/mappu/miqt/qt6"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/config"
	"github.com/rm4n0s/english-tutor/internal/logging"
	"github.com/rm4n0s/english-tutor/internal/store"
)

// Run starts the application and returns its exit code. It creates the Qt
// application, prepares ~/.english-tutor, opens the log and the database, and
// runs the event loop. With a system tray, closing the window hides it and the
// tray's Close item ends the application; without one, closing the window ends it.
func Run(args []string) int {
	qt.NewQApplication(args)
	qt.QCoreApplication_SetApplicationName("English Tutor")

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
	if !a.EnableTray() {
		log.Info("no system tray available, closing the window quits the application")
	}
	code := qt.QApplication_Exec()
	cancel()
	a.Wait() // a check or a save that was still running ends before the database closes
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
	fmt.Fprintln(os.Stderr, "english-tutor:", msg)
	qt.QMessageBox_Critical(nil, "English Tutor", "English Tutor cannot start.\n\n"+msg)
	return 1
}
