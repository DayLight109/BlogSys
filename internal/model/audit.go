package model

import "time"

type AuditLog struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	UserID    uint64    `gorm:"not null;index:idx_audit_user_created,priority:1" json:"userId"`
	Username  string    `gorm:"size:50;not null" json:"username"`
	Method    string    `gorm:"size:10;not null" json:"method"`
	Path      string    `gorm:"size:255;not null" json:"path"`
	Status    int       `gorm:"not null" json:"status"`
	IP        string    `gorm:"size:45" json:"ip,omitempty"`
	UserAgent string    `gorm:"size:500" json:"userAgent,omitempty"`
	CreatedAt time.Time `gorm:"index:idx_audit_user_created,priority:2;index:idx_audit_created" json:"createdAt"`
}

func (AuditLog) TableName() string { return "admin_audit_log" }
