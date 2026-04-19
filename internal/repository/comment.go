package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/lilce/blog-api/internal/model"
)

type CommentRepository struct {
	db *gorm.DB
}

func NewCommentRepository(db *gorm.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

type CommentListQuery struct {
	PostID uint64
	Status string
	Page   int
	Size   int
}

func (q *CommentListQuery) normalize() {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 || q.Size > 100 {
		q.Size = 20
	}
}

func (r *CommentRepository) List(q CommentListQuery) ([]model.Comment, int64, error) {
	q.normalize()
	tx := r.db.Model(&model.Comment{})
	if q.PostID > 0 {
		tx = tx.Where("post_id = ?", q.PostID)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var comments []model.Comment
	err := tx.
		Order("created_at DESC, id DESC").
		Limit(q.Size).
		Offset((q.Page - 1) * q.Size).
		Find(&comments).Error
	return comments, total, err
}

func (r *CommentRepository) FindByID(id uint64) (*model.Comment, error) {
	var c model.Comment
	if err := r.db.First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *CommentRepository) Create(c *model.Comment) error {
	return r.db.Create(c).Error
}

func (r *CommentRepository) UpdateStatus(id uint64, status string) error {
	return r.db.Model(&model.Comment{}).Where("id = ?", id).
		Update("status", status).Error
}

func (r *CommentRepository) Delete(id uint64) error {
	return r.db.Delete(&model.Comment{}, id).Error
}
