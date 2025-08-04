package controllers

import (
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type CamData struct {
	Ip          string  `json:"ip"`
	Port        string  `json:"port"`
	Login       string  `json:"login"`
	Passsword   string  `json:"password"`
	Channels    string  `json:"channels"`
	City        string  `json:"city"`
	City_rus    string  `json:"city_rus"`
	Region      string  `json:"region"`
	Region_rus  string  `json:"region_rus"`
	Country     string  `json:"country"`
	Country_rus string  `json:"country_rus"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	Vuln        string  `json:"vuln"`
}

func GetCams(c *gin.Context) {
	var cams []models.Camera
	if err := initializers.DB.
		Preload("Region").
		Preload("Region.Country").
		Order("city, ip").
		Find(&cams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	type SendCamera struct {
		ID          uint
		Name        string
		Status      string
		IP          string
		Port        string
		Lat         float64
		Lng         float64
		Comment     string
		City        string
		City_rus    string
		Region      string
		Region_rus  string
		Country     string
		Country_rus string
		Images      []string
	}
	var result []SendCamera
	for _, cam := range cams {
		if strings.Contains(cam.Region.Country.Name, os.Getenv("EXCLUDED_COUNTRY")) {
			continue
		}
		var images []string
		entries, err := os.ReadDir(fmt.Sprintf("./data/photos/%s", cam.IP))
		if err == nil {
			for _, entry := range entries {
				images = append(images, url.QueryEscape(entry.Name()))
			}
		}
		result = append(result, SendCamera{
			ID:          cam.ID,
			Name:        cam.Name,
			Status:      cam.Status,
			IP:          cam.IP,
			Port:        cam.Port,
			Lat:         cam.Lat,
			Lng:         cam.Lng,
			Comment:     cam.Comment,
			City:        cam.City,
			City_rus:    cam.City_rus,
			Region:      cam.Region.Name,
			Region_rus:  cam.Region.Name_rus,
			Country:     cam.Region.Country.Name,
			Country_rus: cam.Region.Country.Name_rus,
			Images:      images,
		})
	}

	c.JSON(http.StatusOK, result)
}

func GetCamInfo(c *gin.Context) {
	ip := c.Param("ip")
	port := c.Param("port")

	var camera models.Camera
	if err := initializers.DB.
		Select("name, ip, port, login, password, address, lat, lng, comment").
		Where("ip = ? AND port = ?", ip, port).
		Find(&camera).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while get cam info"})
	}

	c.JSON(http.StatusOK, camera)
}

func SaveComment(c *gin.Context) {
	var body struct {
		Ip      string
		Port    string
		Comment string
	}

	if err := c.Bind(&body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error while receive body request"})
	}

	if err := initializers.DB.Model(&models.Camera{}).Where("ip = ? AND port = ?", body.Ip, body.Port).Update("comment", body.Comment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error while updating data"})
	}

	c.JSON(http.StatusOK, gin.H{})
}

func getOrCreateRegion(countryName, coutnryName_rus, regionName, regionName_rus string) (models.Region, error) {
	var country models.Country
	if err := initializers.DB.
		Where("name = ?", countryName).
		FirstOrCreate(&country, models.Country{Name: countryName, Name_rus: coutnryName_rus}).Error; err != nil {
		return models.Region{}, err
	}

	var region models.Region
	if err := initializers.DB.
		Where("name = ? AND country_id = ?", regionName, country.ID).
		FirstOrCreate(&region, models.Region{Name: regionName, Name_rus: regionName_rus, CountryID: country.ID}).Error; err != nil {
		return models.Region{}, err
	}
	return region, nil
}

func UploadCameras(c *gin.Context) {
	file, _ := c.FormFile("file")

	file_data, _ := file.Open()
	defer file_data.Close()

	jsonBytes, err := io.ReadAll(file_data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while process file"})
		return
	}

	var cam_data []CamData
	if err = json.Unmarshal(jsonBytes, &cam_data); err != nil {
		fmt.Printf("Error while unmarshal json: %s\n", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid json"})
		return
	}

	for _, record := range cam_data {
		region, err := getOrCreateRegion(record.Country, record.Country_rus, record.Region, record.Region_rus)
		if err != nil {
			continue
		}
		camera := models.Camera{
			IP:            record.Ip,
			Port:          record.Port,
			Login:         record.Login,
			Password:      record.Passsword,
			Channels:      record.Channels,
			City:          record.City,
			City_rus:      record.City_rus,
			RegionID:      region.ID,
			Lat:           record.Lat,
			Lng:           record.Lng,
			Vulnerability: record.Vuln,
			Status:        "undefined",
		}

		var existing models.Camera
		result := initializers.DB.Where("ip = ? AND port = ?", camera.IP, camera.Port).First(&existing)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			initializers.DB.Create(&camera)
		}
	}
	c.JSON(http.StatusOK, gin.H{})
}

func GetCamImage(c *gin.Context) {
	ip := c.Param("ip")
	image := c.Param("image")

	c.File(fmt.Sprintf("./data/photos/%s/%s", ip, image))
}

func DefineCam(c *gin.Context) {
	var body struct {
		IP      string
		Port    string
		Name    string
		Coords  string
		Address string
		Comment string
	}

	if err := c.Bind(&body); err != nil {
		fmt.Printf("Error while bind data: %s", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
	}

	fmt.Printf("Binded data: %v\n", body)
	split_coords := strings.Split(body.Coords, ",")
	lat, err := strconv.ParseFloat(strings.Trim(split_coords[0], ", "), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while convert coords"})
	}
	lng, err := strconv.ParseFloat(strings.Trim(split_coords[1], ", "), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while convert coords"})
	}

	if err := initializers.DB.
		Model(&models.Camera{}).
		Where("ip = ? AND port = ?", body.IP, body.Port).
		Updates(models.Camera{Name: body.Name, Lat: lat, Lng: lng, Address: body.Address, Comment: body.Comment, Status: "defined"}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while updating record"})
	}

	c.JSON(http.StatusOK, gin.H{})
}
