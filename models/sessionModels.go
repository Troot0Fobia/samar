package models

import (
	"time"

	"gorm.io/gorm"
)

type Session struct {
	gorm.Model
	UserID    uint      `gorm:"index"`
	User      User      `gorm:"constraint:OnDelete:CASCADE;"`
	TokenHash string    `gorm:"uniqueIndex;size:64"`
	CSRFToken string    `gorm:"size:64"`
	Expires   time.Time `gorm:"index"`
	Active    bool      `gorm:"default:true"`
}
