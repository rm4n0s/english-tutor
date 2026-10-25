// Package instance keeps English Tutor to one running copy per user. The first
// copy holds a lock file and listens on a Unix socket in the data directory (mode
// 0700, so only the user can reach it); a second copy finds the lock taken, asks
// the first one over the socket to show its window, and exits.
package instance

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

const (
	lockName   = "instance.lock"
	socketName = "instance.sock"

	// showCommand is the one line a second copy sends.
	showCommand = "show"

	// readTimeout bounds how long the first copy waits for a client's line, so
	// a client that connects and says nothing cannot hold it.
	readTimeout = 2 * time.Second
	// retryEvery is how often Notify tries again while the first copy is still
	// starting and has not begun to listen.
	retryEvery = 100 * time.Millisecond
)

// Guard is the claim of the first copy. It is released by Close, or by the
// system when the process ends.
type Guard struct {
	lock     *os.File
	dir      *os.File // kept open while the socket exists, see socketPath
	listener net.Listener
	sockPath string

	closeOnce sync.Once
	closeErr  error
	done      sync.WaitGroup
}

// Acquire tries to become the only running copy, using the lock and the socket in
// dir. first is false, with a nil Guard, when another copy holds the lock. When it
// is true, the socket is already listening, so a second copy started a moment
// later can reach this one; call Serve to answer it.
func Acquire(dir string) (g *Guard, first bool, err error) {
	lock, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, errors.NewErr("InstanceLockFailed", err).SetMetadata("dir", dir)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if cerr := lock.Close(); cerr != nil {
			return nil, false, errors.NewErr("InstanceLockFailed", cerr).SetMetadata("dir", dir)
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, errors.NewErr("InstanceLockFailed", err).SetMetadata("dir", dir)
	}

	// Holding the lock means no other copy is running, so a socket file that is
	// still there was left by one that crashed.
	sockPath := filepath.Join(dir, socketName)
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return nil, false, socketFailure(err, sockPath, lock)
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return nil, false, socketFailure(err, sockPath, lock)
	}
	l, err := net.Listen("unix", socketPath(dirFile))
	if err != nil {
		return nil, false, socketFailure(err, sockPath, lock, dirFile)
	}
	return &Guard{lock: lock, dir: dirFile, listener: l, sockPath: sockPath}, true, nil
}

// socketFailure is the error for a socket that could not be made. It closes the
// files the failed attempt holds; a failure to close is added to it.
func socketFailure(err error, sockPath string, files ...*os.File) error {
	appErr := errors.NewErr("InstanceSocketFailed", err).SetMetadata("path", sockPath)
	for _, f := range files {
		if cerr := f.Close(); cerr != nil {
			appErr = appErr.SetMetadata("close", cerr.Error())
		}
	}
	return appErr
}

// socketPath names the socket inside an open directory through /proc, which
// keeps the path short. A socket address holds only about 107 bytes, and the
// data directory sits under a home that can be named at any length. While the
// directory stays open the name works for Listen, for Dial and for the removal of
// the socket file when its listener closes.
func socketPath(dir *os.File) string {
	return fmt.Sprintf("/proc/self/fd/%d/%s", dir.Fd(), socketName)
}

// Serve answers the second copies that call Notify: onShow is called, on a
// goroutine of its own, each time one asks for the window. It returns at once and
// stops when Close is called.
func (g *Guard) Serve(onShow func()) {
	g.done.Add(1)
	go func() {
		defer g.done.Done()
		for {
			conn, err := g.listener.Accept()
			if err != nil {
				return // the listener was closed
			}
			go handle(conn, onShow)
		}
	}()
}

// handle reads the one line of a client and acts on it.
func handle(conn net.Conn, onShow func()) {
	defer func() { _ = conn.Close() }() // a client's connection; nothing is left to do for it
	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return
	}
	if strings.TrimSpace(line) == showCommand {
		onShow()
	}
}

// Close stops Serve, removes the socket and releases the lock. It is safe to call
// more than once.
func (g *Guard) Close() error {
	g.closeOnce.Do(func() {
		g.closeErr = g.listener.Close() // also removes the socket file
		// The directory is closed only now: removing the socket used its name.
		for _, f := range []*os.File{g.dir, g.lock} {
			if err := f.Close(); err != nil && g.closeErr == nil {
				g.closeErr = err
			}
		}
	})
	if g.closeErr != nil {
		return errors.NewErr("InstanceCloseFailed", g.closeErr).SetMetadata("path", g.sockPath)
	}
	return nil
}

// Wait returns once Serve has stopped; call it after Close.
func (g *Guard) Wait() { g.done.Wait() }

// Notify asks the copy that is running to show its window. It keeps trying until
// ctx ends, because the first copy may still be starting.
func Notify(ctx context.Context, dir string) error {
	sockPath := filepath.Join(dir, socketName)
	dirFile, err := os.Open(dir)
	if err != nil {
		return errors.NewErr("InstanceNotifyFailed", err).SetMetadata("path", sockPath)
	}
	err = dial(ctx, dirFile, sockPath)
	if cerr := dirFile.Close(); cerr != nil && err == nil {
		return errors.NewErr("InstanceNotifyFailed", cerr).SetMetadata("path", sockPath)
	}
	return err
}

// dial connects to the socket in dirFile, trying again until ctx ends, and sends
// the command.
func dial(ctx context.Context, dirFile *os.File, sockPath string) error {
	var d net.Dialer
	for {
		conn, err := d.DialContext(ctx, "unix", socketPath(dirFile))
		if err == nil {
			return send(conn, sockPath)
		}
		select {
		case <-ctx.Done():
			return errors.NewErr("InstanceNotifyFailed", err).SetMetadata("path", sockPath)
		case <-time.After(retryEvery):
		}
	}
}

// send writes the command to the first copy and closes the connection.
func send(conn net.Conn, sockPath string) error {
	_, werr := conn.Write([]byte(showCommand + "\n"))
	cerr := conn.Close()
	if werr != nil {
		return errors.NewErr("InstanceNotifyFailed", werr).SetMetadata("path", sockPath)
	}
	if cerr != nil {
		return errors.NewErr("InstanceNotifyFailed", cerr).SetMetadata("path", sockPath)
	}
	return nil
}
