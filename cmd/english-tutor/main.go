// Command english-tutor is a desktop writing tutor that underlines English
// errors found by an LLM.
package main

import (
	"fmt"
	"os"

	"github.com/rm4n0s/english-tutor/internal/config"
	"github.com/rm4n0s/english-tutor/internal/logging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "english-tutor:", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := config.HomeDir()
	if err != nil {
		return err
	}
	paths := config.NewPaths(home)
	if err := paths.Ensure(); err != nil {
		return err
	}
	logger, closer := logging.New(paths, os.Stderr)
	defer func() {
		if err := closer.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "english-tutor: closing log:", err)
		}
	}()
	logger.Info("english-tutor starting", "data_dir", paths.DataDir)
	return nil
}
