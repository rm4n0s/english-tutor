package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Settings keys.
const (
	KeySelectedLLMID   = "selected_llm_id"
	KeyAutoCheck       = "auto_check"
	KeyAutoCheckDelayS = "auto_check_delay_s"
	KeyFontSize        = "font_size"
	KeySplitterSizes   = "splitter_sizes"
	KeyWindowGeometry  = "window_geometry"
)

// Defaults used when a setting is missing or damaged.
const (
	DefaultAutoCheck    = false
	DefaultAutoCheckSec = 3
	DefaultFontSize     = 13
)

// GetSetting returns the value stored under key and whether it exists.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var st Setting
	err := s.db.WithContext(ctx).First(&st, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errors.NewErr("DatabaseQueryFailed", err).SetMetadata("key", key)
	}
	return st.Value, true, nil
}

// SetSetting stores value under key, replacing any previous value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Setting{Key: key, Value: value}).Error
	if err != nil {
		return errors.NewErr("DatabaseQueryFailed", err).SetMetadata("key", key)
	}
	return nil
}

// A stored value that cannot be parsed is treated as missing, so a damaged
// setting never stops the application from starting.

// SelectedLLMID returns the id of the selected LLM, or 0 if none is selected.
func (s *Store) SelectedLLMID(ctx context.Context) (uint, error) {
	v, ok, err := s.GetSetting(ctx, KeySelectedLLMID)
	if err != nil || !ok {
		return 0, err
	}
	id, perr := strconv.ParseUint(v, 10, 32)
	if perr != nil {
		return 0, nil
	}
	return uint(id), nil
}

// SetSelectedLLMID remembers the selected LLM. Pass 0 for none.
func (s *Store) SetSelectedLLMID(ctx context.Context, id uint) error {
	return s.SetSetting(ctx, KeySelectedLLMID, strconv.FormatUint(uint64(id), 10))
}

// AutoCheck reports whether auto-check is on. It is off by default.
func (s *Store) AutoCheck(ctx context.Context) (bool, error) {
	v, ok, err := s.GetSetting(ctx, KeyAutoCheck)
	if err != nil || !ok {
		return DefaultAutoCheck, err
	}
	on, perr := strconv.ParseBool(v)
	if perr != nil {
		return DefaultAutoCheck, nil
	}
	return on, nil
}

// SetAutoCheck turns auto-check on or off.
func (s *Store) SetAutoCheck(ctx context.Context, on bool) error {
	return s.SetSetting(ctx, KeyAutoCheck, strconv.FormatBool(on))
}

// AutoCheckDelaySeconds returns the pause, in seconds, after which auto-check
// runs. The default is 3.
func (s *Store) AutoCheckDelaySeconds(ctx context.Context) (int, error) {
	return s.positiveInt(ctx, KeyAutoCheckDelayS, DefaultAutoCheckSec)
}

// SetAutoCheckDelaySeconds stores the auto-check delay.
func (s *Store) SetAutoCheckDelaySeconds(ctx context.Context, seconds int) error {
	return s.SetSetting(ctx, KeyAutoCheckDelayS, strconv.Itoa(seconds))
}

// FontSize returns the editor font size in points. The default is 13.
func (s *Store) FontSize(ctx context.Context) (int, error) {
	return s.positiveInt(ctx, KeyFontSize, DefaultFontSize)
}

// SetFontSize stores the editor font size.
func (s *Store) SetFontSize(ctx context.Context, points int) error {
	return s.SetSetting(ctx, KeyFontSize, strconv.Itoa(points))
}

// SplitterSizes returns the saved splitter sizes, or nil if none are saved.
func (s *Store) SplitterSizes(ctx context.Context) ([]int, error) {
	v, ok, err := s.GetSetting(ctx, KeySplitterSizes)
	if err != nil || !ok {
		return nil, err
	}
	var sizes []int
	if jerr := json.Unmarshal([]byte(v), &sizes); jerr != nil {
		return nil, nil
	}
	return sizes, nil
}

// SetSplitterSizes stores the splitter sizes.
func (s *Store) SetSplitterSizes(ctx context.Context, sizes []int) error {
	b, err := json.Marshal(sizes)
	if err != nil {
		return errors.NewErr("DatabaseQueryFailed", err).SetMetadata("key", KeySplitterSizes)
	}
	return s.SetSetting(ctx, KeySplitterSizes, string(b))
}

// WindowGeometry returns the saved window geometry bytes, or nil if none.
func (s *Store) WindowGeometry(ctx context.Context) ([]byte, error) {
	v, ok, err := s.GetSetting(ctx, KeyWindowGeometry)
	if err != nil || !ok {
		return nil, err
	}
	b, derr := base64.StdEncoding.DecodeString(v)
	if derr != nil {
		return nil, nil
	}
	return b, nil
}

// SetWindowGeometry stores the window geometry bytes.
func (s *Store) SetWindowGeometry(ctx context.Context, geometry []byte) error {
	return s.SetSetting(ctx, KeyWindowGeometry, base64.StdEncoding.EncodeToString(geometry))
}

func (s *Store) positiveInt(ctx context.Context, key string, def int) (int, error) {
	v, ok, err := s.GetSetting(ctx, key)
	if err != nil || !ok {
		return def, err
	}
	n, perr := strconv.Atoi(v)
	if perr != nil || n < 1 {
		return def, nil
	}
	return n, nil
}
