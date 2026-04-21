package service

import (
	"errors"
	"sort"
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
	Pinned   *bool
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
	if in.Pinned != nil {
		p.Pinned = *in.Pinned
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
	if in.Pinned != nil {
		p.Pinned = *in.Pinned
	}

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

// GetNeighbors returns prev/next published posts around the given slug.
func (s *PostService) GetNeighbors(slug string) (*model.Post, *model.Post, error) {
	current, err := s.posts.FindBySlug(slug)
	if err != nil {
		return nil, nil, err
	}
	if current == nil || current.Status != model.PostStatusPublished {
		return nil, nil, ErrPostNotFound
	}
	return s.posts.FindNeighbors(current.ID, current.PublishedAt)
}

// GetRelated returns up to `limit` other published posts that share tags with the given slug.
func (s *PostService) GetRelated(slug string, limit int) ([]model.Post, error) {
	if limit <= 0 {
		limit = 3
	}
	current, err := s.posts.FindBySlug(slug)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Status != model.PostStatusPublished {
		return nil, ErrPostNotFound
	}
	if len(current.Tags) == 0 {
		return nil, nil
	}
	return s.posts.FindRelated([]string(current.Tags), current.ID, limit)
}

// ArchiveEntry is one year in the archive page.
type ArchiveEntry struct {
	Year  int          `json:"year"`
	Posts []model.Post `json:"posts"`
}

// Archive groups published posts by year (DESC).
func (s *PostService) Archive() ([]ArchiveEntry, error) {
	posts, err := s.posts.Archive()
	if err != nil {
		return nil, err
	}
	byYear := make(map[int][]model.Post)
	years := make([]int, 0)
	for _, p := range posts {
		t := p.CreatedAt
		if p.PublishedAt != nil {
			t = *p.PublishedAt
		}
		y := t.Year()
		if _, ok := byYear[y]; !ok {
			years = append(years, y)
		}
		byYear[y] = append(byYear[y], p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	out := make([]ArchiveEntry, 0, len(years))
	for _, y := range years {
		out = append(out, ArchiveEntry{Year: y, Posts: byYear[y]})
	}
	return out, nil
}

// Search — FULLTEXT over title + content_md.
func (s *PostService) Search(q string, page, size int) ([]model.Post, int64, error) {
	return s.posts.Search(q, page, size)
}

// TagCount is an aggregated tag with its post count.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ListTags aggregates all tags from published posts with counts, sorted by count DESC then name ASC.
func (s *PostService) ListTags() ([]TagCount, error) {
	tagArrays, err := s.posts.AllTagsFromPublished()
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, arr := range tagArrays {
		for _, t := range arr {
			counts[t]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for t, c := range counts {
		out = append(out, TagCount{Tag: t, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	return out, nil
}

// RenameTag replaces `from` with `to` in every post's tags array (deduped).
func (s *PostService) RenameTag(from, to string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || to == "" {
		return errors.New("tag name required")
	}
	return s.posts.RenameTag(from, to)
}

// MergeTags replaces any occurrence of tags in `from` with `to`.
func (s *PostService) MergeTags(from []string, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return errors.New("target tag required")
	}
	cleaned := make([]string, 0, len(from))
	for _, f := range from {
		f = strings.TrimSpace(f)
		if f != "" && f != to {
			cleaned = append(cleaned, f)
		}
	}
	if len(cleaned) == 0 {
		return errors.New("source tags required")
	}
	return s.posts.MergeTags(cleaned, to)
}

// DeleteTag removes `name` from every post's tags array.
func (s *PostService) DeleteTag(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("tag name required")
	}
	return s.posts.DeleteTag(name)
}

// GetPublishedByID retrieves a published post used by admin reply to cache post existence.
func (s *PostService) GetPublishedByID(id uint64) (*model.Post, error) {
	p, err := s.posts.FindByID(id)
	if err != nil {
		return nil, err
	}
	if p == nil || p.Status != model.PostStatusPublished {
		return nil, ErrPostNotFound
	}
	return p, nil
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
