package helpers

import (
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"

	"gorm.io/gorm"
)

func GetProxy() (models.Proxy, error) {
	var proxy models.Proxy

	if err := initializers.DB.Where("active = ?", true).First(&proxy).Error; err != nil {
		return models.Proxy{}, err
	}

	return proxy, nil
}

func UpdateProxyUsageCount(count int, proxyUrl string) error {
	if err := initializers.DB.
		Model(&models.Proxy{}).
		Where("proxy_url = ?", proxyUrl).
		UpdateColumn("usage_count", gorm.Expr("usage_count + ?", count)).
		Error; err != nil {
		return err
	}
	return nil
}
