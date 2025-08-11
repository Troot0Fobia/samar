package models

import "gorm.io/gorm"

type Proxy struct {
	gorm.Model
	ProxyUrl   string `gorm:"unique"`
	UsageCount int    `gorm:"default:0"`
	Active     bool   `gorm:"default:true"`
}
