package models

import (
	"time"

	"gorm.io/gorm"
)

type InviteToken struct {
	gorm.Model
	Token   string `gorm:"uniqueIndex"`
	Role    string
	Expires time.Time
	Used    bool `gorm:"default:false"`
}
