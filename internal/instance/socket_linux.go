//go:build linux

package instance

import (
	"fmt"
	"os"
)

// socketPath names the socket inside an open directory through /proc, which
// keeps the path short. A socket address holds only about 107 bytes, and the
// data directory sits under a home that can be named at any length. While the
// directory stays open the name works for Listen, for Dial and for the removal of
// the socket file when its listener closes.
func socketPath(dir *os.File) (string, error) {
	return fmt.Sprintf("/proc/self/fd/%d/%s", dir.Fd(), socketName), nil
}
