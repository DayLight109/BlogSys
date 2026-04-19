package model

import "time"

type User struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:50;uniqueIndex;not null" json:"username"`
	Email        *string   `gorm:"size:100;uniqueIndex" json:"email,omitempty"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	DisplayName  *string   `gorm:"size:100" json:"displayName,omitempty"`
	Role         string    `gorm:"size:20;not null;default:admin" json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (User) TableName() string { return "users" }
