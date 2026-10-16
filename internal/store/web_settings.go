package store

import (
	"context"
	"strconv"

	"github.com/rm4n0s/errors"
)

// Settings keys of the web server (section 4.7 of the spec).
const (
	KeyWebPort     = "web_port"
	KeyWebPassword = "web_password"
)

// DefaultWebPort is the port the web server listens on until the user changes it.
const DefaultWebPort = 3333

// WebPort returns the port of the web server. A missing or damaged value gives
// the default, 3333.
func (s *Store) WebPort(ctx context.Context) (int, error) {
	v, ok, err := s.GetSetting(ctx, KeyWebPort)
	if err != nil || !ok {
		return DefaultWebPort, err
	}
	n, perr := strconv.Atoi(v)
	if perr != nil || ValidateWebPort(n) != nil {
		return DefaultWebPort, nil
	}
	return n, nil
}

// SetWebPort stores the port of the web server.
//
// Error: WebPortInvalid.
func (s *Store) SetWebPort(ctx context.Context, port int) error {
	if err := ValidateWebPort(port); err != nil {
		return err
	}
	return s.SetSetting(ctx, KeyWebPort, strconv.Itoa(port))
}

// ValidateWebPort checks that port can be listened on. The message of the
// returned error is meant to be shown to the user.
func ValidateWebPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("WebPortInvalid", "The port must be a number from 1 to 65535.", "port", port)
	}
	return nil
}

// WebPasswordHash returns the stored hash of the web server's password, or ""
// when no password is set. The hash is made by the web package; the password
// itself is never stored.
func (s *Store) WebPasswordHash(ctx context.Context) (string, error) {
	v, _, err := s.GetSetting(ctx, KeyWebPassword)
	return v, err
}

// SetWebPasswordHash stores the hash of the web server's password. An empty hash
// removes the password.
func (s *Store) SetWebPasswordHash(ctx context.Context, hash string) error {
	return s.SetSetting(ctx, KeyWebPassword, hash)
}
