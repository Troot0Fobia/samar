package models

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Username string `gorm:"unique"`
	PassHash string
	Role     string    `gorm:"default:user"`
	Active   bool      `gorm:"default:true"`
	Sessions []Session `gorm:"foreignKey:UserID"`
}
