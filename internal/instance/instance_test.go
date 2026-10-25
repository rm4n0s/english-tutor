package instance

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// shortDir returns a folder whose path is short enough for a Unix socket.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "et")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func acquireFirst(t *testing.T, dir string) *Guard {
	t.Helper()
	g, first, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !first || g == nil {
		t.Fatalf("Acquire() first = %v, guard = %v; want the first copy", first, g)
	}
	return g
}

func closeGuard(t *testing.T, g *Guard) {
	t.Helper()
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	g.Wait()
}

func TestOnlyOneCopyHoldsTheLock(t *testing.T) {
	dir := shortDir(t)
	g := acquireFirst(t, dir)

	second, first, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first || second != nil {
		t.Fatalf("second Acquire() first = %v, guard = %v; want it refused", first, second)
	}

	// Once the first copy ends, the next one can take over.
	closeGuard(t, g)
	closeGuard(t, acquireFirst(t, dir))
}

func TestNotifyAsksTheFirstCopyToShow(t *testing.T) {
	dir := shortDir(t)
	g := acquireFirst(t, dir)
	shown := make(chan struct{}, 4)
	g.Serve(func() { shown <- struct{}{} })

	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := Notify(ctx, dir)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-shown:
		case <-time.After(2 * time.Second):
			t.Fatal("the first copy was not asked to show its window")
		}
	}
	closeGuard(t, g)
	if _, err := os.Stat(filepath.Join(dir, socketName)); !os.IsNotExist(err) {
		t.Errorf("socket after Close: err = %v, want it removed", err)
	}
}

func TestOtherLinesDoNothing(t *testing.T) {
	dir := shortDir(t)
	g := acquireFirst(t, dir)
	shown := make(chan struct{}, 1)
	g.Serve(func() { shown <- struct{}{} })

	tests := []struct {
		name string
		send string
	}{
		{name: "unknown command", send: "quit\n"},
		{name: "no line break", send: "show"},
		{name: "nothing", send: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := net.Dial("unix", filepath.Join(dir, socketName))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write([]byte(tt.send)); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	select {
	case <-shown:
		t.Error("a line that is not \"show\" showed the window")
	case <-time.After(200 * time.Millisecond):
	}
	closeGuard(t, g)
}

func TestLeftoverSocketOfACrashedCopy(t *testing.T) {
	dir := shortDir(t)
	if err := os.WriteFile(filepath.Join(dir, socketName), []byte("left behind"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := acquireFirst(t, dir)
	shown := make(chan struct{}, 1)
	g.Serve(func() { shown <- struct{}{} })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Notify(ctx, dir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-shown:
	case <-time.After(2 * time.Second):
		t.Fatal("the new first copy did not answer")
	}
	closeGuard(t, g)
}

func TestDataDirectoryWithALongPath(t *testing.T) {
	// A socket address holds about 107 bytes; the folder's name is longer than that.
	dir := filepath.Join(shortDir(t), strings.Repeat("d", 150))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	g := acquireFirst(t, dir)
	shown := make(chan struct{}, 1)
	g.Serve(func() { shown <- struct{}{} })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Notify(ctx, dir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-shown:
	case <-time.After(2 * time.Second):
		t.Fatal("the first copy was not asked to show its window")
	}
	closeGuard(t, g)
	if _, err := os.Stat(filepath.Join(dir, socketName)); !os.IsNotExist(err) {
		t.Errorf("socket after Close: err = %v, want it removed", err)
	}
}

func TestCloseTwice(t *testing.T) {
	g := acquireFirst(t, shortDir(t))
	closeGuard(t, g)
	if err := g.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
}

func TestFailures(t *testing.T) {
	tests := []struct {
		name      string
		run       func(t *testing.T) error
		wantRoute string
	}{
		{
			name: "the lock file cannot be opened",
			run: func(t *testing.T) error {
				t.Helper()
				_, _, err := Acquire(filepath.Join(shortDir(t), "missing"))
				return err
			},
			wantRoute: "Acquire.InstanceLockFailed",
		},
		{
			name: "the leftover socket cannot be removed",
			run: func(t *testing.T) error {
				t.Helper()
				dir := shortDir(t)
				if err := os.MkdirAll(filepath.Join(dir, socketName, "x"), 0o700); err != nil {
					t.Fatal(err)
				}
				_, _, err := Acquire(dir)
				return err
			},
			wantRoute: "Acquire->socketFailure.InstanceSocketFailed",
		},
		{
			name: "nobody is listening",
			run: func(t *testing.T) error {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
				defer cancel()
				return Notify(ctx, shortDir(t))
			},
			wantRoute: "Notify->dial.InstanceNotifyFailed",
		},
		{
			name: "the folder is gone",
			run: func(t *testing.T) error {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
				defer cancel()
				return Notify(ctx, filepath.Join(shortDir(t), "missing"))
			},
			wantRoute: "Notify.InstanceNotifyFailed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.wantRoute) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestCloseFailure(t *testing.T) {
	g := acquireFirst(t, shortDir(t))
	// Closing the lock file behind the guard's back makes its own Close fail.
	if err := g.lock.Close(); err != nil {
		t.Fatal(err)
	}
	err := g.Close()
	g.Wait()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("Guard.Close.InstanceCloseFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
