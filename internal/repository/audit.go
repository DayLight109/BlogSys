package repository

import (
	"gorm.io/gorm"

	"github.com/lilce/blog-api/internal/model"
)

type AuditRepository struct {
	db *gorm.DB
}

func NewAuditRepository(db *gorm.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) Create(e *model.AuditLog) error {
	return r.db.Create(e).Error
}

func (r *AuditRepository) List(page, size int) ([]model.AuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	tx := r.db.Model(&model.AuditLog{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.AuditLog
	err := tx.Order("created_at DESC, id DESC").
		Limit(size).Offset((page - 1) * size).Find(&items).Error
	return items, total, err
}
