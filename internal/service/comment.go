package service

import (
	"errors"

	"github.com/lilce/blog-api/internal/model"
	"github.com/lilce/blog-api/internal/repository"
)

var ErrCommentNotFound = errors.New("comment not found")

type CommentService struct {
	comments *repository.CommentRepository
	posts    *repository.PostRepository
	settings *SettingService
}

func NewCommentService(comments *repository.CommentRepository, posts *repository.PostRepository, settings *SettingService) *CommentService {
	return &CommentService{comments: comments, posts: posts, settings: settings}
}

type CommentInput struct {
	PostID        uint64
	ParentID      *uint64
	AuthorName    string
	AuthorEmail   *string
	AuthorWebsite *string
	Content       string
	IP            *string
	UserAgent     *string
}

func (s *CommentService) Submit(in CommentInput) (*model.Comment, error) {
	p, err := s.posts.FindByID(in.PostID)
	if err != nil {
		return nil, err
	}
	if p == nil || p.Status != model.PostStatusPublished {
		return nil, ErrPostNotFound
	}
	if in.ParentID != nil {
		parent, err := s.comments.FindByID(*in.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil || parent.PostID != in.PostID {
			return nil, errors.New("invalid parent comment")
		}
	}

	c := &model.Comment{
		PostID:        in.PostID,
		ParentID:      in.ParentID,
		AuthorName:    in.AuthorName,
		AuthorEmail:   in.AuthorEmail,
		AuthorWebsite: in.AuthorWebsite,
		Content:       in.Content,
		Status:        model.CommentStatusPending,
		IP:            in.IP,
		UserAgent:     in.UserAgent,
	}
	if err := s.comments.Create(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *CommentService) ListApprovedForPost(postID uint64, page, size int) ([]model.Comment, int64, error) {
	return s.comments.List(repository.CommentListQuery{
		PostID: postID,
		Status: model.CommentStatusApproved,
		Page:   page,
		Size:   size,
	})
}

func (s *CommentService) ListAdmin(status string, postID uint64, page, size int) ([]model.Comment, int64, error) {
	return s.comments.List(repository.CommentListQuery{
		PostID: postID,
		Status: status,
		Page:   page,
		Size:   size,
	})
}

func (s *CommentService) UpdateStatus(id uint64, status string) error {
	c, err := s.comments.FindByID(id)
	if err != nil {
		return err
	}
	if c == nil {
		return ErrCommentNotFound
	}
	switch status {
	case model.CommentStatusPending, model.CommentStatusApproved, model.CommentStatusSpam:
	default:
		return errors.New("invalid status")
	}
	return s.comments.UpdateStatus(id, status)
}

func (s *CommentService) Delete(id uint64) error {
	return s.comments.Delete(id)
}

// AdminReply posts an auto-approved comment authored by the site owner.
// postID must reference an existing post; parentID (optional) must belong to the same post.
func (s *CommentService) AdminReply(postID uint64, parentID *uint64, content string) (*model.Comment, error) {
	p, err := s.posts.FindByID(postID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrPostNotFound
	}
	if parentID != nil {
		parent, err := s.comments.FindByID(*parentID)
		if err != nil {
			return nil, err
		}
		if parent == nil || parent.PostID != postID {
			return nil, errors.New("invalid parent comment")
		}
	}
	authorName := "Author"
	if s.settings != nil {
		if n, err := s.settings.BrandName(); err == nil && n != "" {
			authorName = n
		}
	}
	c := &model.Comment{
		PostID:     postID,
		ParentID:   parentID,
		AuthorName: authorName,
		Content:    content,
		Status:     model.CommentStatusApproved,
	}
	if err := s.comments.Create(c); err != nil {
		return nil, err
	}
	return c, nil
}
