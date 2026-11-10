package app

import (
	"os"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestSetupDesktopProgramNotFound(t *testing.T) {
	_, err := setupDesktop(t.TempDir(), func() (string, error) { return "", os.ErrNotExist })
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("setupDesktop.ExecutablePathNotFound") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
