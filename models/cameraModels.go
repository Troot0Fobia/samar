package models

import "gorm.io/gorm"

type Country struct {
	gorm.Model
	Name     string `gorm:"unique"`
	Name_rus string `gorm:"unique"`
	Regions  []Region
}

type Region struct {
	gorm.Model
	Name      string `gorm:"index:idx_country_region,unique"`
	Name_rus  string
	CountryID uint `gorm:"index:idx_country_region,unique"`
	Country   Country
	Cameras   []Camera
}

type Camera struct {
	gorm.Model
	Name          string
	Status        string
	IP            string `gorm:"uniqueIndex:idx_ip_port"`
	Port          string `gorm:"uniqueIndex:idx_ip_port"`
	Login         string
	Password      string
	Address       string
	Lat           float64 `gorm:"index"`
	Lng           float64 `gorm:"index"`
	Channels      string
	Comment       string
	Vulnerability string
	City          string
	City_rus      string
	RegionID      uint
	Region        Region
}
