package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/lilce/blog-api/internal/model"
)

type PostRepository struct {
	db *gorm.DB
}

func NewPostRepository(db *gorm.DB) *PostRepository {
	return &PostRepository{db: db}
}

type PostListQuery struct {
	Status string
	Tag    string
	Page   int
	Size   int
}

func (q *PostListQuery) normalize() {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 || q.Size > 50 {
		q.Size = 10
	}
}

func (r *PostRepository) List(q PostListQuery) ([]model.Post, int64, error) {
	q.normalize()
	tx := r.db.Model(&model.Post{})
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.Tag != "" {
		tx = tx.Where("JSON_CONTAINS(tags, ?)", `"`+q.Tag+`"`)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var posts []model.Post
	err := tx.
		Order("COALESCE(published_at, created_at) DESC, id DESC").
		Limit(q.Size).
		Offset((q.Page - 1) * q.Size).
		Find(&posts).Error
	return posts, total, err
}

func (r *PostRepository) FindBySlug(slug string) (*model.Post, error) {
	var p model.Post
	if err := r.db.Preload("Author").Where("slug = ?", slug).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *PostRepository) FindByID(id uint64) (*model.Post, error) {
	var p model.Post
	if err := r.db.Preload("Author").First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *PostRepository) SlugExists(slug string, excludeID uint64) (bool, error) {
	var n int64
	tx := r.db.Model(&model.Post{}).Where("slug = ?", slug)
	if excludeID > 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	err := tx.Count(&n).Error
	return n > 0, err
}

func (r *PostRepository) Create(p *model.Post) error {
	return r.db.Create(p).Error
}

func (r *PostRepository) Update(p *model.Post) error {
	return r.db.Save(p).Error
}

func (r *PostRepository) Delete(id uint64) error {
	return r.db.Delete(&model.Post{}, id).Error
}

func (r *PostRepository) IncrementView(id uint64) error {
	return r.db.Model(&model.Post{}).Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
}
