// Package config resolves where English Tutor keeps its data on disk.
package config

import (
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

const (
	dataDirName = ".english-tutor"
	dbFileName  = "english-tutor.db"
	logDirName  = "logs"
	logFileName = "english-tutor.log"

	// DirMode is the mode of the data and log directories.
	DirMode os.FileMode = 0o700
	// FileMode is the mode of the database and log files.
	FileMode os.FileMode = 0o600
)

// Paths holds every location the application writes to.
type Paths struct {
	DataDir string
	DBFile  string
	LogDir  string
	LogFile string
}

// HomeDir returns the user's home directory.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.NewErr("HomeDirNotFound", err)
	}
	return home, nil
}

// NewPaths builds the paths under home/.english-tutor. It touches no files.
func NewPaths(home string) Paths {
	dataDir := filepath.Join(home, dataDirName)
	logDir := filepath.Join(dataDir, logDirName)
	return Paths{
		DataDir: dataDir,
		DBFile:  filepath.Join(dataDir, dbFileName),
		LogDir:  logDir,
		LogFile: filepath.Join(logDir, logFileName),
	}
}

// Ensure creates the data directory with mode 0700 and tightens the mode of an
// existing one. The log directory is created by the logging package.
func (p Paths) Ensure() error {
	if err := os.MkdirAll(p.DataDir, DirMode); err != nil {
		return errors.NewErr("DataDirCreateFailed", err).SetMetadata("path", p.DataDir)
	}
	if err := os.Chmod(p.DataDir, DirMode); err != nil {
		return errors.NewErr("DataDirCreateFailed", err).SetMetadata("path", p.DataDir)
	}
	return nil
}
