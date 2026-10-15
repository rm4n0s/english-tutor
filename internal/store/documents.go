package store

import (
	"context"

	"github.com/rm4n0s/errors"
	"gorm.io/gorm"
)

// CreateDocument inserts doc and sets doc.ID.
func (s *Store) CreateDocument(ctx context.Context, doc *Document) error {
	doc.ID = 0
	if err := s.db.WithContext(ctx).Create(doc).Error; err != nil {
		return errors.NewErr("DatabaseQueryFailed", err)
	}
	return nil
}

// UpdateDocument saves the title and content of doc, found by doc.ID.
func (s *Store) UpdateDocument(ctx context.Context, doc *Document) error {
	res := s.db.WithContext(ctx).Model(&Document{ID: doc.ID}).
		Select("Title", "Content").
		Updates(doc)
	if res.Error != nil {
		return errors.NewErr("DatabaseQueryFailed", res.Error)
	}
	if res.RowsAffected == 0 {
		return errors.New("DocumentNotFound", "This document no longer exists in the library.", "id", doc.ID)
	}
	return nil
}

// GetDocument returns the document with the given id.
func (s *Store) GetDocument(ctx context.Context, id uint) (*Document, error) {
	var doc Document
	if err := s.db.WithContext(ctx).First(&doc, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("DocumentNotFound", "This document no longer exists in the library.", "id", id)
		}
		return nil, errors.NewErr("DatabaseQueryFailed", err)
	}
	return &doc, nil
}

// ListDocuments returns every document, most recently updated first.
func (s *Store) ListDocuments(ctx context.Context) ([]Document, error) {
	var docs []Document
	if err := s.db.WithContext(ctx).Order("updated_at desc").Order("id desc").Find(&docs).Error; err != nil {
		return nil, errors.NewErr("DatabaseQueryFailed", err)
	}
	return docs, nil
}

// DeleteDocument removes the document with the given id.
func (s *Store) DeleteDocument(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&Document{}, id)
	if res.Error != nil {
		return errors.NewErr("DatabaseQueryFailed", res.Error)
	}
	if res.RowsAffected == 0 {
		return errors.New("DocumentNotFound", "This document no longer exists in the library.", "id", id)
	}
	return nil
}
