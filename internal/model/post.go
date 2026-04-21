package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return json.Marshal(a)
}

func (a *StringArray) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("StringArray: unsupported scan type")
	}
	return json.Unmarshal(b, a)
}

const (
	PostStatusDraft     = "draft"
	PostStatusPublished = "published"
	PostStatusArchived  = "archived"
)

type Post struct {
	ID          uint64      `gorm:"primaryKey" json:"id"`
	Title       string      `gorm:"size:255;not null" json:"title"`
	Slug        string      `gorm:"size:255;uniqueIndex;not null" json:"slug"`
	Summary     *string     `gorm:"size:500" json:"summary,omitempty"`
	ContentMD   string      `gorm:"type:mediumtext;not null" json:"contentMd"`
	ContentHTML string      `gorm:"type:mediumtext;not null" json:"contentHtml"`
	CoverURL    *string     `gorm:"size:500" json:"coverUrl,omitempty"`
	Status      string      `gorm:"size:20;not null;default:draft;index" json:"status"`
	Tags        StringArray `gorm:"type:json" json:"tags"`
	Pinned      bool        `gorm:"not null;default:false;index" json:"pinned"`
	AuthorID    uint64      `gorm:"not null;index" json:"authorId"`
	Author      *User       `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	ViewCount   uint32      `gorm:"not null;default:0" json:"viewCount"`
	PublishedAt *time.Time  `json:"publishedAt,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

func (Post) TableName() string { return "posts" }
