package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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
		Order("pinned DESC, COALESCE(published_at, created_at) DESC, id DESC").
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

// FindNeighbors returns the previous and next published posts for a given post,
// ordered by published_at. Either may be nil when at the edges of the timeline.
func (r *PostRepository) FindNeighbors(currentID uint64, publishedAt *time.Time) (*model.Post, *model.Post, error) {
	if publishedAt == nil {
		return nil, nil, nil
	}

	var prev, next model.Post
	errPrev := r.db.
		Where("status = ? AND id <> ? AND published_at < ?", model.PostStatusPublished, currentID, publishedAt).
		Order("published_at DESC, id DESC").
		First(&prev).Error
	errNext := r.db.
		Where("status = ? AND id <> ? AND published_at > ?", model.PostStatusPublished, currentID, publishedAt).
		Order("published_at ASC, id ASC").
		First(&next).Error

	var pPrev, pNext *model.Post
	if errPrev == nil {
		pPrev = &prev
	} else if !errors.Is(errPrev, gorm.ErrRecordNotFound) {
		return nil, nil, errPrev
	}
	if errNext == nil {
		pNext = &next
	} else if !errors.Is(errNext, gorm.ErrRecordNotFound) {
		return nil, nil, errNext
	}
	return pPrev, pNext, nil
}

// FindRelated returns up to `limit` published posts sharing any tag with the given tag list,
// excluding `excludeID`. Ranked by (tag overlap DESC, published_at DESC). MySQL 5.7 compatible
// (no JSON_OVERLAPS) — we OR together JSON_CONTAINS for each tag and compute match count client-side.
func (r *PostRepository) FindRelated(tags []string, excludeID uint64, limit int) ([]model.Post, error) {
	if len(tags) == 0 || limit <= 0 {
		return nil, nil
	}

	tx := r.db.Model(&model.Post{}).
		Where("status = ? AND id <> ?", model.PostStatusPublished, excludeID)

	conds := r.db.Session(&gorm.Session{})
	for i, t := range tags {
		b, err := json.Marshal(t)
		if err != nil {
			continue
		}
		if i == 0 {
			conds = conds.Where("JSON_CONTAINS(tags, ?)", string(b))
		} else {
			conds = conds.Or("JSON_CONTAINS(tags, ?)", string(b))
		}
	}
	tx = tx.Where(conds)

	// Fetch up to limit*3 candidates, score in Go, return top N.
	var pool []model.Post
	if err := tx.Order("published_at DESC, id DESC").Limit(limit * 3).Find(&pool).Error; err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		set[t] = struct{}{}
	}

	type scored struct {
		post  model.Post
		score int
	}
	ss := make([]scored, 0, len(pool))
	for _, p := range pool {
		s := 0
		for _, t := range p.Tags {
			if _, ok := set[t]; ok {
				s++
			}
		}
		if s > 0 {
			ss = append(ss, scored{post: p, score: s})
		}
	}
	sort.SliceStable(ss, func(i, j int) bool {
		if ss[i].score != ss[j].score {
			return ss[i].score > ss[j].score
		}
		pi, pj := ss[i].post.PublishedAt, ss[j].post.PublishedAt
		if pi != nil && pj != nil {
			return pi.After(*pj)
		}
		return ss[i].post.CreatedAt.After(ss[j].post.CreatedAt)
	})

	if len(ss) > limit {
		ss = ss[:limit]
	}
	out := make([]model.Post, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.post)
	}
	return out, nil
}

// Archive returns all published posts, most-recent first. Caller groups by year.
func (r *PostRepository) Archive() ([]model.Post, error) {
	var posts []model.Post
	err := r.db.
		Select("id, title, slug, summary, status, tags, published_at, created_at").
		Where("status = ?", model.PostStatusPublished).
		Order("published_at DESC, id DESC").
		Find(&posts).Error
	return posts, err
}

// Search runs a MySQL FULLTEXT search over title + content_md with the ngram parser,
// falling back to LIKE when the query is too short for FULLTEXT.
func (r *PostRepository) Search(q string, page, size int) ([]model.Post, int64, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 50 {
		size = 20
	}

	tx := r.db.Model(&model.Post{}).
		Where("status = ?", model.PostStatusPublished)

	// ngram parser handles CJK down to 2-char; also works for latin.
	if utf8.RuneCountInString(q) >= 2 {
		tx = tx.Where("MATCH(title, content_md) AGAINST (? IN NATURAL LANGUAGE MODE)", q)
	} else {
		like := "%" + q + "%"
		tx = tx.Where("title LIKE ? OR content_md LIKE ?", like, like)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var posts []model.Post
	err := tx.
		Order("COALESCE(published_at, created_at) DESC, id DESC").
		Limit(size).
		Offset((page - 1) * size).
		Find(&posts).Error
	return posts, total, err
}

// AllTagsFromPublished scans tag JSON arrays across published posts for aggregation.
// Small-scale blog friendly (100s of posts OK).
func (r *PostRepository) AllTagsFromPublished() ([]model.StringArray, error) {
	var posts []model.Post
	err := r.db.Model(&model.Post{}).
		Select("id, tags").
		Where("status = ?", model.PostStatusPublished).
		Find(&posts).Error
	if err != nil {
		return nil, err
	}
	out := make([]model.StringArray, 0, len(posts))
	for _, p := range posts {
		out = append(out, p.Tags)
	}
	return out, nil
}

// findPostsContainingTag loads all posts whose tags JSON array contains `tag`.
func (r *PostRepository) findPostsContainingTag(tx *gorm.DB, tag string) ([]model.Post, error) {
	tagJSON, err := json.Marshal(tag)
	if err != nil {
		return nil, err
	}
	var posts []model.Post
	if err := tx.Model(&model.Post{}).
		Select("id, tags").
		Where("JSON_CONTAINS(tags, ?)", string(tagJSON)).
		Find(&posts).Error; err != nil {
		return nil, err
	}
	return posts, nil
}

// replaceTagInSlice removes any occurrence of `from` and adds `to` (deduped).
// Returns the new slice and whether a change occurred.
func replaceTagInSlice(tags []string, from, to string) ([]string, bool) {
	found := false
	seen := make(map[string]struct{}, len(tags)+1)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if t == from {
			found = true
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if !found {
		return tags, false
	}
	if to != "" {
		if _, ok := seen[to]; !ok {
			out = append(out, to)
		}
	}
	return out, true
}

// RenameTag updates every post that has `from` in its tags JSON array, replacing it with `to`.
func (r *PostRepository) RenameTag(from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		posts, err := r.findPostsContainingTag(tx, from)
		if err != nil {
			return err
		}
		for _, p := range posts {
			newTags, changed := replaceTagInSlice([]string(p.Tags), from, to)
			if !changed {
				continue
			}
			if err := tx.Model(&model.Post{}).Where("id = ?", p.ID).
				Update("tags", model.StringArray(newTags)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// MergeTags replaces any of `from` with `to` across all posts, deduped.
func (r *PostRepository) MergeTags(from []string, to string) error {
	if to == "" || len(from) == 0 {
		return nil
	}
	fromSet := make(map[string]struct{}, len(from))
	for _, f := range from {
		if f != "" && f != to {
			fromSet[f] = struct{}{}
		}
	}
	if len(fromSet) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Load candidate posts: any that contain at least one of the from tags.
		seen := make(map[uint64]struct{})
		var all []model.Post
		for f := range fromSet {
			posts, err := r.findPostsContainingTag(tx, f)
			if err != nil {
				return err
			}
			for _, p := range posts {
				if _, ok := seen[p.ID]; ok {
					continue
				}
				seen[p.ID] = struct{}{}
				all = append(all, p)
			}
		}
		for _, p := range all {
			rewritten := make([]string, 0, len(p.Tags))
			dedupe := make(map[string]struct{})
			replaced := false
			for _, t := range p.Tags {
				if _, ok := fromSet[t]; ok {
					replaced = true
					continue
				}
				if _, ok := dedupe[t]; ok {
					continue
				}
				dedupe[t] = struct{}{}
				rewritten = append(rewritten, t)
			}
			if replaced {
				if _, ok := dedupe[to]; !ok {
					rewritten = append(rewritten, to)
				}
			}
			if err := tx.Model(&model.Post{}).Where("id = ?", p.ID).
				Update("tags", model.StringArray(rewritten)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteTag removes `name` from every post's tags JSON array.
func (r *PostRepository) DeleteTag(name string) error {
	if name == "" {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		posts, err := r.findPostsContainingTag(tx, name)
		if err != nil {
			return err
		}
		for _, p := range posts {
			newTags, changed := replaceTagInSlice([]string(p.Tags), name, "")
			if !changed {
				continue
			}
			if err := tx.Model(&model.Post{}).Where("id = ?", p.ID).
				Update("tags", model.StringArray(newTags)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
