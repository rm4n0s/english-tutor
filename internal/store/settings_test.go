package store

import (
	"context"
	"reflect"
	"testing"
)

func TestSettingDefaults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if on, err := s.AutoCheck(ctx); err != nil || on {
		t.Errorf("AutoCheck() = %v, %v; want false (off by default)", on, err)
	}
	if n, err := s.AutoCheckDelaySeconds(ctx); err != nil || n != 3 {
		t.Errorf("AutoCheckDelaySeconds() = %d, %v; want 3", n, err)
	}
	if n, err := s.FontSize(ctx); err != nil || n != 13 {
		t.Errorf("FontSize() = %d, %v; want 13", n, err)
	}
	if id, err := s.SelectedLLMID(ctx); err != nil || id != 0 {
		t.Errorf("SelectedLLMID() = %d, %v; want 0", id, err)
	}
	if sizes, err := s.SplitterSizes(ctx); err != nil || sizes != nil {
		t.Errorf("SplitterSizes() = %v, %v; want nil", sizes, err)
	}
	if g, err := s.WindowGeometry(ctx); err != nil || g != nil {
		t.Errorf("WindowGeometry() = %v, %v; want nil", g, err)
	}
}

func TestSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tests := []struct {
		name string
		set  func() error
		get  func() (any, error)
		want any
	}{
		{"auto check on", func() error { return s.SetAutoCheck(ctx, true) }, func() (any, error) { return s.AutoCheck(ctx) }, true},
		{"auto check off again", func() error { return s.SetAutoCheck(ctx, false) }, func() (any, error) { return s.AutoCheck(ctx) }, false},
		{"delay", func() error { return s.SetAutoCheckDelaySeconds(ctx, 8) }, func() (any, error) { return s.AutoCheckDelaySeconds(ctx) }, 8},
		{"font size", func() error { return s.SetFontSize(ctx, 16) }, func() (any, error) { return s.FontSize(ctx) }, 16},
		{"selected llm", func() error { return s.SetSelectedLLMID(ctx, 7) }, func() (any, error) { return s.SelectedLLMID(ctx) }, uint(7)},
		{"selected llm replaced", func() error { return s.SetSelectedLLMID(ctx, 0) }, func() (any, error) { return s.SelectedLLMID(ctx) }, uint(0)},
		{"splitter sizes", func() error { return s.SetSplitterSizes(ctx, []int{600, 300}) }, func() (any, error) { return s.SplitterSizes(ctx) }, []int{600, 300}},
		{"window geometry", func() error { return s.SetWindowGeometry(ctx, []byte{0, 1, 2, 255}) }, func() (any, error) { return s.WindowGeometry(ctx) }, []byte{0, 1, 2, 255}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.set(); err != nil {
				t.Fatal(err)
			}
			got, err := tt.get()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestSettingDamagedValuesFallBack(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for k, v := range map[string]string{
		KeyAutoCheck: "maybe", KeyAutoCheckDelayS: "-4", KeyFontSize: "big",
		KeySelectedLLMID: "x", KeySplitterSizes: "{", KeyWindowGeometry: "***",
	} {
		if err := s.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if on, err := s.AutoCheck(ctx); err != nil || on {
		t.Errorf("AutoCheck() = %v, %v", on, err)
	}
	if n, err := s.AutoCheckDelaySeconds(ctx); err != nil || n != 3 {
		t.Errorf("AutoCheckDelaySeconds() = %d, %v", n, err)
	}
	if n, err := s.FontSize(ctx); err != nil || n != 13 {
		t.Errorf("FontSize() = %d, %v", n, err)
	}
	if id, err := s.SelectedLLMID(ctx); err != nil || id != 0 {
		t.Errorf("SelectedLLMID() = %d, %v", id, err)
	}
	if sizes, err := s.SplitterSizes(ctx); err != nil || sizes != nil {
		t.Errorf("SplitterSizes() = %v, %v", sizes, err)
	}
	if g, err := s.WindowGeometry(ctx); err != nil || g != nil {
		t.Errorf("WindowGeometry() = %v, %v", g, err)
	}
}

func TestSettingQueryFailures(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		run   func(s *Store) error
		route string
	}{
		{"get", func(s *Store) error { _, _, err := s.GetSetting(ctx, "k"); return err }, "Store.GetSetting.DatabaseQueryFailed"},
		{"set", func(s *Store) error { return s.SetSetting(ctx, "k", "v") }, "Store.SetSetting.DatabaseQueryFailed"},
		{"typed get", func(s *Store) error { _, err := s.AutoCheck(ctx); return err }, "Store.GetSetting.DatabaseQueryFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRoute(t, tt.run(closedStore(t)), tt.route)
		})
	}
}
