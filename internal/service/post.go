package service

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/lilce/blog-api/internal/markdown"
	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
)

var (
	ErrPostNotFound = errors.New("post not found")
	ErrSlugTaken    = errors.New("slug already taken")
)

type PostService struct {
	posts *repository.PostRepository
}

func NewPostService(posts *repository.PostRepository) *PostService {
	return &PostService{posts: posts}
}

type PostInput struct {
	Title    string
	Slug     string
	Summary  *string
	Content  string
	CoverURL *string
	Status   string
	Tags     []string
	Publish  bool
}

func (s *PostService) ListPublic(tag string, page, size int) ([]model.Post, int64, error) {
	return s.posts.List(repository.PostListQuery{
		Status: model.PostStatusPublished,
		Tag:    tag,
		Page:   page,
		Size:   size,
	})
}

func (s *PostService) ListAdmin(status, tag string, page, size int) ([]model.Post, int64, error) {
	return s.posts.List(repository.PostListQuery{
		Status: status,
		Tag:    tag,
		Page:   page,
		Size:   size,
	})
}

func (s *PostService) GetPublishedBySlug(slug string) (*model.Post, error) {
	p, err := s.posts.FindBySlug(slug)
	if err != nil {
		return nil, err
	}
	if p == nil || p.Status != model.PostStatusPublished {
		return nil, ErrPostNotFound
	}
	_ = s.posts.IncrementView(p.ID)
	return p, nil
}

func (s *PostService) GetByID(id uint64) (*model.Post, error) {
	p, err := s.posts.FindByID(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrPostNotFound
	}
	return p, nil
}

func (s *PostService) Create(authorID uint64, in PostInput) (*model.Post, error) {
	slug := normalizeSlug(in.Slug, in.Title)
	exists, err := s.posts.SlugExists(slug, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrSlugTaken
	}

	html, err := markdown.Render(in.Content)
	if err != nil {
		return nil, err
	}

	status := in.Status
	if status == "" {
		status = model.PostStatusDraft
	}
	var publishedAt *time.Time
	if in.Publish || status == model.PostStatusPublished {
		now := time.Now()
		publishedAt = &now
		status = model.PostStatusPublished
	}

	p := &model.Post{
		Title:       in.Title,
		Slug:        slug,
		Summary:     in.Summary,
		ContentMD:   in.Content,
		ContentHTML: html,
		CoverURL:    in.CoverURL,
		Status:      status,
		Tags:        model.StringArray(in.Tags),
		AuthorID:    authorID,
		PublishedAt: publishedAt,
	}
	if err := s.posts.Create(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PostService) Update(id uint64, in PostInput) (*model.Post, error) {
	p, err := s.posts.FindByID(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrPostNotFound
	}

	slug := normalizeSlug(in.Slug, in.Title)
	if slug != p.Slug {
		exists, err := s.posts.SlugExists(slug, p.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrSlugTaken
		}
		p.Slug = slug
	}

	html, err := markdown.Render(in.Content)
	if err != nil {
		return nil, err
	}

	p.Title = in.Title
	p.Summary = in.Summary
	p.ContentMD = in.Content
	p.ContentHTML = html
	p.CoverURL = in.CoverURL
	p.Tags = model.StringArray(in.Tags)

	wasPublished := p.Status == model.PostStatusPublished
	if in.Status != "" {
		p.Status = in.Status
	}
	if in.Publish && !wasPublished {
		now := time.Now()
		p.PublishedAt = &now
		p.Status = model.PostStatusPublished
	}

	if err := s.posts.Update(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PostService) Delete(id uint64) error {
	return s.posts.Delete(id)
}

func normalizeSlug(slug, fallbackTitle string) string {
	if slug == "" {
		slug = fallbackTitle
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(slug)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "post-" + time.Now().Format("20060102150405")
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}
