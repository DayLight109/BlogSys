package model

import "time"

type Setting struct {
	Key       string    `gorm:"primaryKey;size:64" json:"key"`
	Value     string    `gorm:"type:json;not null" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Setting) TableName() string { return "site_settings" }
