package repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lilce/blog-api/internal/model"
)

type SettingRepository struct {
	db *gorm.DB
}

func NewSettingRepository(db *gorm.DB) *SettingRepository {
	return &SettingRepository{db: db}
}

func (r *SettingRepository) All() ([]model.Setting, error) {
	var items []model.Setting
	err := r.db.Find(&items).Error
	return items, err
}

func (r *SettingRepository) Get(key string) (*model.Setting, error) {
	var s model.Setting
	if err := r.db.Where("`key` = ?", key).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// Upsert writes key → value (value must be a valid JSON string).
func (r *SettingRepository) Upsert(key, jsonValue string) error {
	s := model.Setting{Key: key, Value: jsonValue}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&s).Error
}

// UpsertIfAbsent writes only when the key doesn't already exist — used for seeding defaults.
func (r *SettingRepository) UpsertIfAbsent(key, jsonValue string) error {
	existing, err := r.Get(key)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	return r.Upsert(key, jsonValue)
}
