package store

import (
	"context"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

// CreateLLMConfig validates and inserts cfg and sets cfg.ID.
func (s *Store) CreateLLMConfig(ctx context.Context, cfg *LLMConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	cfg.ID = 0
	if err := s.db.WithContext(ctx).Create(cfg).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return errors.New("LLMConfigNameTaken", "An LLM with this name already exists.", "name", cfg.Name)
		}
		return errors.NewErr("DatabaseQueryFailed", err)
	}
	return nil
}

// UpdateLLMConfig validates and saves every editable field of cfg, found by
// cfg.ID. CreatedAt is kept.
func (s *Store) UpdateLLMConfig(ctx context.Context, cfg *LLMConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&LLMConfig{ID: cfg.ID}).
		Select("Name", "Provider", "Model", "BaseURL", "APIKey", "Temperature").
		Updates(cfg)
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
			return errors.New("LLMConfigNameTaken", "An LLM with this name already exists.", "name", cfg.Name)
		}
		return errors.NewErr("DatabaseQueryFailed", res.Error)
	}
	if res.RowsAffected == 0 {
		return errors.New("LLMConfigNotFound", "This LLM no longer exists.", "id", cfg.ID)
	}
	return nil
}

// GetLLMConfig returns the configuration with the given id.
func (s *Store) GetLLMConfig(ctx context.Context, id uint) (*LLMConfig, error) {
	var cfg LLMConfig
	if err := s.db.WithContext(ctx).First(&cfg, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("LLMConfigNotFound", "This LLM no longer exists.", "id", id)
		}
		return nil, errors.NewErr("DatabaseQueryFailed", err)
	}
	return &cfg, nil
}

// ListLLMConfigs returns every configuration ordered by name.
func (s *Store) ListLLMConfigs(ctx context.Context) ([]LLMConfig, error) {
	var cfgs []LLMConfig
	if err := s.db.WithContext(ctx).Order("name").Find(&cfgs).Error; err != nil {
		return nil, errors.NewErr("DatabaseQueryFailed", err)
	}
	return cfgs, nil
}

// DeleteLLMConfig removes the configuration with the given id.
func (s *Store) DeleteLLMConfig(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&LLMConfig{}, id)
	if res.Error != nil {
		return errors.NewErr("DatabaseQueryFailed", res.Error)
	}
	if res.RowsAffected == 0 {
		return errors.New("LLMConfigNotFound", "This LLM no longer exists.", "id", id)
	}
	return nil
}
