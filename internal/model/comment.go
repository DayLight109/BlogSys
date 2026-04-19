package model

import "time"

const (
	CommentStatusPending  = "pending"
	CommentStatusApproved = "approved"
	CommentStatusSpam     = "spam"
)

type Comment struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	PostID        uint64    `gorm:"not null;index" json:"postId"`
	ParentID      *uint64   `gorm:"index" json:"parentId,omitempty"`
	AuthorName    string    `gorm:"size:100;not null" json:"authorName"`
	AuthorEmail   *string   `gorm:"size:100" json:"authorEmail,omitempty"`
	AuthorWebsite *string   `gorm:"size:200" json:"authorWebsite,omitempty"`
	Content       string    `gorm:"type:text;not null" json:"content"`
	Status        string    `gorm:"size:20;not null;default:pending;index" json:"status"`
	IP            *string   `gorm:"size:45" json:"-"`
	UserAgent     *string   `gorm:"size:500" json:"-"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`

	Replies []Comment `gorm:"foreignKey:ParentID" json:"replies,omitempty"`
}

func (Comment) TableName() string { return "comments" }
