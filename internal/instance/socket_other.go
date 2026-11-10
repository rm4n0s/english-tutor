//go:build !linux

package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/rm4n0s/errors"
)

// maxSocketPath is how long the path of a socket may be on the systems without /proc: macOS holds 104 bytes in a socket address
// (with the end byte), Linux 108; the smaller is used, with some room.
const maxSocketPath = 100

// socketPath is where the socket is: in the data directory when that path fits an address, which is the case of the usual
// folder (`~/.english-tutor`), and otherwise in a folder of the user's temporary directory, under a name that the data
// directory's path makes (macOS's temporary directory is private to the user; the folder is made with mode 0700 and its owner
// and mode are checked, so another user cannot put a socket of their own in its place).
func socketPath(dir *os.File) (string, error) {
	direct := filepath.Join(dir.Name(), socketName)
	if len(direct) <= maxSocketPath {
		return direct, nil
	}
	folder := filepath.Join(os.TempDir(), fmt.Sprintf("english-tutor-%d", os.Getuid()))
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return "", errors.NewErr("InstanceSocketDirFailed", err).SetMetadata("dir", folder)
	}
	if err := checkPrivate(folder, syscall.Lstat, os.Getuid()); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(dir.Name()))
	return filepath.Join(folder, hex.EncodeToString(sum[:8])+".sock"), nil
}

// checkPrivate says that folder is a directory of the user uid that nobody else can enter. lstat is syscall.Lstat; a test
// replaces it.
func checkPrivate(folder string, lstat func(string, *syscall.Stat_t) error, uid int) error {
	var st syscall.Stat_t
	if err := lstat(folder, &st); err != nil {
		return errors.NewErr("InstanceSocketDirUnreadable", err).SetMetadata("dir", folder)
	}
	if int(st.Uid) != uid || st.Mode&0o077 != 0 || st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return errors.New("InstanceSocketDirUnsafe", "the folder of the socket is not a private folder of this user", "dir", folder)
	}
	return nil
}
