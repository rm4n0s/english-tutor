package store

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/english-tutor/internal/llm"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenMemory(context.Background())
	if err != nil {
		t.Fatalf("OpenMemory() error = %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return s
}

// closedStore returns a store whose database is already closed, so every
// query fails.
func closedStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenMemory(context.Background())
	if err != nil {
		t.Fatalf("OpenMemory() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return s
}

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with route %q, got nil", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func validConfig(name string) *LLMConfig {
	return &LLMConfig{
		Name:        name,
		Provider:    string(llm.ProviderOllama),
		Model:       "llama3.1",
		BaseURL:     "http://localhost:11434",
		Temperature: DefaultTemperature,
	}
}

func mustAppErr(t *testing.T, err error) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	return appErr
}

func errMetadata(t *testing.T, err error, key string) any {
	t.Helper()
	return mustAppErr(t, err).Metadata[key]
}
