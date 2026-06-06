package models

import "gorm.io/gorm"

type Country struct {
	gorm.Model
	Name     string `gorm:"uniqueIndex:idx_country_name"`
	Name_rus string `gorm:"column:name_rus"`
	Regions  []Region
}

type Region struct {
	gorm.Model
	Key       string `gorm:"index:idx_region_key_country,unique"`
	Name      string
	Name_rus  string `gorm:"column:name_rus"`
	CountryID uint   `gorm:"index:idx_region_key_country,unique"`
	Country   Country
	Cameras   []Camera
	Cities    []City
}

type City struct {
	gorm.Model
	Key      string `gorm:"uniqueIndex:idx_city_key_region"` // composite unique index with RegionID
	Name     string
	Name_rus string `gorm:"column:name_rus"`
	RegionID uint   `gorm:"uniqueIndex:idx_city_key_region"`
}

type Maintainer struct {
	gorm.Model
	Name    string `gorm:"unique"`
	Cameras []Camera
}

type Camera struct {
	gorm.Model
	Name          string
	IsDefined     bool
	Status        string // "valid" | "invalid" | "duplicate" | "undetectable"
	IP            string `gorm:"uniqueIndex:idx_ip_port"`
	Port          string `gorm:"uniqueIndex:idx_ip_port"`
	Login         string
	Password      string
	Address       string
	Link          string
	Lat           float64 `gorm:"index"`
	Lng           float64 `gorm:"index"`
	Channels      string
	Comment       string
	RtspLink      string
	RegionID      uint
	Region        Region
	CityID        *uint
	CityRef       *City       `gorm:"foreignKey:CityID"`       // non-conventional name: avoids collision with Region.Cities
	MaintainerID  *uint
	MaintainerRef *Maintainer `gorm:"foreignKey:MaintainerID"` // non-conventional name: avoids collision with models.Maintainer type
	CanonicalID   *uint
	Images        []string    `gorm:"-"`
}
