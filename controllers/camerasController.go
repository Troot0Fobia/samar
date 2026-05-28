package controllers

import (
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type CamData struct {
	Ip          string  `json:"ip"`
	Port        string  `json:"port"`
	Login       string  `json:"login"`
	Password    string  `json:"password"`
	Channels    string  `json:"channels"`
	City        string  `json:"city"`
	City_rus    string  `json:"city_rus"`
	Region      string  `json:"region"`
	Region_rus  string  `json:"region_rus"`
	Country     string  `json:"country"`
	Country_rus string  `json:"country_rus"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

type CamUploadResult struct {
	IP     string `json:"ip"`
	Port   string `json:"port"`
	Region string `json:"region"`
	City   string `json:"city"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type NewCityEntry struct {
	Name    string `json:"name"`
	NameRus string `json:"name_rus"`
}

type UploadReport struct {
	Results    []CamUploadResult `json:"results"`
	NewCities  []NewCityEntry    `json:"new_cities"`
	AddedCount int               `json:"added_count"`
	DupCount   int               `json:"dup_count"`
	ErrorCount int               `json:"error_count"`
}

func GetCams(c *gin.Context) {
	var cams []models.Camera
	_, role, username := middleware.CheckAuth(c)
	if err := initializers.DB.
		Preload("Region").
		Preload("Region.Country").
		Preload("MaintainerRef").
		Preload("CityRef").
		Joins("LEFT JOIN cities ON cities.id = cameras.city_id").
		Order("cities.name, cameras.ip").
		Find(&cams).Error; err != nil {
		helpers.LogError("Error with receiving cams from database", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	type SendCamera struct {
		ID           uint     `json:"ID"`
		Name         string   `json:"Name"`
		IsDefined    bool     `json:"IsDefined"`
		Status       string   `json:"Status"`
		IP           string   `json:"IP"`
		Port         string   `json:"Port"`
		Lat          float64  `json:"Lat"`
		Lng          float64  `json:"Lng"`
		Comment      string   `json:"Comment"`
		City         string   `json:"City"`
		City_rus     string   `json:"City_rus"`
		Region       string   `json:"Region"`
		Region_rus   string   `json:"Region_rus"`
		Country      string   `json:"Country"`
		Country_rus  string   `json:"Country_rus"`
		Images       []string `json:"Images"`
		MaintainerID *uint    `json:"MaintainerID"`
		Maintainer   string   `json:"Maintainer"`
	}
	// Pre-load directory listings once per unique IP to avoid N+1 os.ReadDir calls.
	type dirEntry struct {
		name  string
		isDir bool
	}
	photoCache := make(map[string][]dirEntry)
	for _, cam := range cams {
		if _, ok := photoCache[cam.IP]; !ok {
			entries, err := os.ReadDir(fmt.Sprintf("./data/photos/%s", cam.IP))
			if err != nil {
				photoCache[cam.IP] = nil
			} else {
				de := make([]dirEntry, 0, len(entries))
				for _, e := range entries {
					de = append(de, dirEntry{name: e.Name(), isDir: e.IsDir()})
				}
				photoCache[cam.IP] = de
			}
		}
	}

	var result []SendCamera
	excludedCountry := os.Getenv("EXCLUDED_COUNTRY")
	for _, cam := range cams {
		if excludedCountry != "" && strings.Contains(cam.Region.Country.Name, excludedCountry) {
			continue
		}
		if role == "user" && (cam.Status == "invalid" || cam.Status == "duplicate" || cam.Status == "undetectable") {
			continue
		}
		var images []string
		prefix := fmt.Sprintf("%s_%s", cam.IP, cam.Port)
		for _, entry := range photoCache[cam.IP] {
			if entry.isDir {
				continue
			}
			if strings.HasPrefix(entry.name, prefix) && strings.HasSuffix(strings.ToLower(entry.name), ".jpg") {
				images = append(images, url.QueryEscape(entry.name))
			}
		}
		maintainerName := ""
		if cam.MaintainerRef != nil {
			maintainerName = cam.MaintainerRef.Name
		}
		result = append(result, SendCamera{
			ID:           cam.ID,
			Name:         cam.Name,
			IsDefined:    cam.IsDefined,
			Status:       cam.Status,
			IP:           cam.IP,
			Port:         cam.Port,
			Lat:          cam.Lat,
			Lng:          cam.Lng,
			Comment:      cam.Comment,
			City:         camCity(cam.CityRef),
			City_rus:     camCityRus(cam.CityRef),
			Region:       cam.Region.Name,
			Region_rus:   cam.Region.Name_rus,
			Country:      cam.Region.Country.Name,
			Country_rus:  cam.Region.Country.Name_rus,
			Images:       images,
			MaintainerID: cam.MaintainerID,
			Maintainer:   maintainerName,
		})
	}

	if result == nil {
		result = []SendCamera{}
	}
	c.JSON(http.StatusOK, result)
}

func GetCamInfo(c *gin.Context) {
	ip := c.Param("ip")
	port := c.Param("port")

	_, _, username := middleware.CheckAuth(c)

	var camera models.Camera
	if err := initializers.DB.
		Preload("CityRef").
		Preload("MaintainerRef").
		Select("id, name, ip, port, login, password, status, is_defined, address, lat, lng, comment, link, city_id, region_id, maintainer_id").
		Where("ip = ? AND port = ?", ip, port).
		First(&camera).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
			return
		}
		helpers.LogError(fmt.Sprintf("Error with receiving cam info (%s:%s) from database", ip, port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while get cam info"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ID":           camera.ID,
		"IP":           camera.IP,
		"Port":         camera.Port,
		"Name":         camera.Name,
		"Login":        camera.Login,
		"Password":     camera.Password,
		"Lat":          camera.Lat,
		"Lng":          camera.Lng,
		"Comment":      camera.Comment,
		"Address":      camera.Address,
		"Link":         camera.Link,
		"Status":       camera.Status,
		"IsDefined":    camera.IsDefined,
		"CityID":       camera.CityID,
		"City":         camCity(camera.CityRef),
		"City_rus":     camCityRus(camera.CityRef),
		"RegionID":     camera.RegionID,
		"MaintainerID": camera.MaintainerID,
		"Maintainer":   camera.MaintainerRef,
	})
}

func UpdateCamData(c *gin.Context) {
	var body struct {
		Ip           string   `json:"ip"`
		Port         string   `json:"port"`
		Login        string   `json:"login"`
		Password     string   `json:"password"`
		Comment      string   `json:"comment"`
		Name         string   `json:"name"`
		Link         string   `json:"link"`
		Lat          *float64 `json:"lat"`
		Lng          *float64 `json:"lng"`
		CityID       *uint    `json:"city_id"`
		MaintainerID *uint    `json:"maintainer_id"`
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.BindJSON(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}

	if body.Ip == "" || body.Port == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port can not be empty"})
		return
	}

	if !helpers.ValidateIP(body.Ip) || !helpers.ValidatePort(body.Port) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port are invalid"})
		return
	}

	if body.Login == "" || body.Password == "" || body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "login or password or cam name can not be empty"})
		return
	}

	// Fetch existing camera to check region change
	var existingCam models.Camera
	if err := initializers.DB.Preload("CityRef").Where("ip = ? AND port = ?", body.Ip, body.Port).First(&existingCam).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Camera (%s:%s) not found", body.Ip, body.Port), username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "камера не найдена"})
		return
	}

	updates := map[string]any{
		"Login":    body.Login,
		"Password": body.Password,
		"Comment":  body.Comment,
		"Name":     body.Name,
		"Link":     body.Link,
	}

	// Resolve region once if coordinates changed; reuse for city validation below.
	currentRegionID := existingCam.RegionID
	if body.Lat != nil && body.Lng != nil {
		regionName, regionRus := helpers.DetectPolygonByPoint(*body.Lng, *body.Lat)
		country := os.Getenv("MAIN_COUNTRY")
		countryRus := os.Getenv("MAIN_COUNTRY_RUS")
		region, err := helpers.GetOrCreateRegion(country, countryRus, regionName, regionRus)
		if err != nil {
			helpers.LogError("Error with creating or receiving region", username, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating or receiving location"})
			return
		}

		updates["Lat"] = *body.Lat
		updates["Lng"] = *body.Lng
		updates["RegionID"] = region.ID
		currentRegionID = region.ID

		if body.CityID == nil && existingCam.CityID != nil && region.ID != existingCam.RegionID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Регион изменился, город сброшен. Выберите город заново"})
			return
		}
	}

	if body.CityID != nil {
		var cityRec models.City
		if err := initializers.DB.Where("id = ? AND region_id = ?", *body.CityID, currentRegionID).First(&cityRec).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "выбранный город не найден в регионе"})
			return
		}
		updates["CityID"] = *body.CityID
	}

	if body.MaintainerID != nil {
		updates["MaintainerID"] = *body.MaintainerID
	}

	if err := initializers.DB.Model(&models.Camera{}).
		Where("ip = ? AND port = ?", body.Ip, body.Port).
		Updates(updates).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Error with updating cam (%s:%s) data", body.Ip, body.Port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error while updating cam data"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Data for camera (%s:%s) changed successfully", body.Ip, body.Port), username)

	// Fetch updated camera for response
	var updatedCam models.Camera
	if err := initializers.DB.Preload("Region").Preload("Region.Country").Preload("MaintainerRef").Preload("CityRef").Where("ip = ? AND port = ?", body.Ip, body.Port).First(&updatedCam).Error; err != nil {
		// Fallback: return only coords if available, otherwise empty ok
		resp := gin.H{}
		if body.Lat != nil && body.Lng != nil {
			resp["lat"] = *body.Lat
			resp["lng"] = *body.Lng
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	maintainerName := ""
	if updatedCam.MaintainerRef != nil {
		maintainerName = updatedCam.MaintainerRef.Name
	}
	c.JSON(http.StatusOK, gin.H{
		"ID":           updatedCam.ID,
		"Name":         updatedCam.Name,
		"IsDefined":    updatedCam.IsDefined,
		"Status":       updatedCam.Status,
		"IP":           updatedCam.IP,
		"Port":         updatedCam.Port,
		"lat":          updatedCam.Lat,
		"lng":          updatedCam.Lng,
		"Comment":      updatedCam.Comment,
		"Link":         updatedCam.Link,
		"City":         camCity(updatedCam.CityRef),
		"City_rus":     camCityRus(updatedCam.CityRef),
		"Region":       updatedCam.Region.Name,
		"Region_rus":   updatedCam.Region.Name_rus,
		"Country":      updatedCam.Region.Country.Name,
		"Country_rus":  updatedCam.Region.Country.Name_rus,
		"MaintainerID": updatedCam.MaintainerID,
		"Maintainer":   maintainerName,
	})
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func camCity(ref *models.City) string {
	if ref != nil {
		return ref.Name
	}
	return ""
}

func camCityRus(ref *models.City) string {
	if ref != nil {
		return ref.Name_rus
	}
	return ""
}

func UploadCameras(c *gin.Context) {
	_, _, username := middleware.CheckAuth(c)

	file, err := c.FormFile("file")
	if err != nil || file == nil {
		helpers.LogError("No file provided in upload_cams request", username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	if !strings.HasSuffix(file.Filename, ".json") {
		helpers.LogError("File does not satisfied established format", username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file"})
		return
	}

	file_data, err := file.Open()
	if err != nil {
		helpers.LogError("Error opening uploaded file", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while process file"})
		return
	}
	defer file_data.Close()

	jsonBytes, err := io.ReadAll(file_data)
	if err != nil {
		helpers.LogError("Error read file with cameras", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while process file"})
		return
	}

	var cam_data []CamData
	if err = json.Unmarshal(jsonBytes, &cam_data); err != nil {
		helpers.LogError("Error while unmarshal json file", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid json"})
		return
	}

	report := UploadReport{
		Results:   make([]CamUploadResult, 0, len(cam_data)),
		NewCities: []NewCityEntry{},
	}
	seenNewCities := make(map[uint]bool)

	for _, record := range cam_data {
		res := CamUploadResult{IP: record.Ip, Port: record.Port}

		region, err := helpers.GetOrCreateRegion(record.Country, record.Country_rus, record.Region, record.Region_rus)
		if err != nil {
			res.Status = "error"
			res.Error = fmt.Sprintf("не удалось определить регион (страна: %q, регион: %q): %v", record.Country, record.Region, err)
			report.ErrorCount++
			report.Results = append(report.Results, res)
			continue
		}
		if region.Name_rus != "" {
			res.Region = region.Name_rus
		} else {
			res.Region = region.Name
		}

		var uploadCityID *uint
		if record.City != "" {
			cityRec, isNew, cityErr := helpers.GetOrCreateCity(record.City, record.City_rus, region.ID)
			if cityErr == nil && cityRec.ID != 0 {
				cid := cityRec.ID
				uploadCityID = &cid
				if cityRec.Name_rus != "" {
					res.City = cityRec.Name_rus
				} else {
					res.City = cityRec.Name
				}
				if isNew && !seenNewCities[cityRec.ID] {
					seenNewCities[cityRec.ID] = true
					report.NewCities = append(report.NewCities, NewCityEntry{
						Name:    cityRec.Name,
						NameRus: cityRec.Name_rus,
					})
				}
			}
		}

		camera := models.Camera{
			IP:        record.Ip,
			Port:      record.Port,
			Login:     record.Login,
			Password:  record.Password,
			Channels:  record.Channels,
			CityID:    uploadCityID,
			RegionID:  region.ID,
			Lat:       record.Lat,
			Lng:       record.Lng,
			IsDefined: false,
			Status:    "valid",
		}

		var existing models.Camera
		dbResult := initializers.DB.Where("ip = ? AND port = ?", camera.IP, camera.Port).First(&existing)
		if errors.Is(dbResult.Error, gorm.ErrRecordNotFound) {
			if err := initializers.DB.Create(&camera).Error; err != nil {
				helpers.LogError(fmt.Sprintf("Error creating camera (%s:%s) during upload", camera.IP, camera.Port), username, err.Error())
				res.Status = "error"
				res.Error = err.Error()
				report.ErrorCount++
			} else {
				res.Status = "added"
				report.AddedCount++
			}
		} else {
			res.Status = "duplicate"
			report.DupCount++
		}
		report.Results = append(report.Results, res)
	}
	helpers.LogSuccess(fmt.Sprintf("Upload complete: %d added, %d dup, %d error", report.AddedCount, report.DupCount, report.ErrorCount), username)
	c.JSON(http.StatusOK, report)
}

func GetCamImage(c *gin.Context) {
	ip := c.Param("ip")
	image := c.Param("image")

	if !helpers.ValidateIP(ip) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	baseDir, err := filepath.Abs("./data/photos")
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// filepath.Base strips all directory components from image,
	// filepath.Join + Abs then resolves the canonical path.
	filePath, err := filepath.Abs(filepath.Join(baseDir, ip, filepath.Base(image)))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Ensure the resolved path stays inside the photos directory.
	// Add separator so "/data/photos_extra/..." doesn't match "/data/photos".
	if !strings.HasPrefix(filePath, baseDir+string(filepath.Separator)) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	c.File(filePath)
}

func DefineCam(c *gin.Context) {
	var body struct {
		IP           string   `json:"ip"`
		Port         string   `json:"port"`
		Login        string   `json:"login"`
		Password     string   `json:"password"`
		Name         string   `json:"name"`
		Lat          *float64 `json:"lat"`
		Lng          *float64 `json:"lng"`
		Address      string   `json:"address"`
		Comment      string   `json:"comment"`
		CityID       *uint    `json:"city_id"`
		MaintainerID *uint    `json:"maintainer_id"`
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}

	if !helpers.ValidateIP(body.IP) || !helpers.ValidatePort(body.Port) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port are invalid"})
		return
	}

	if body.Lat == nil || body.Lng == nil || !isFinite(*body.Lat) || !isFinite(*body.Lng) {
		helpers.LogError("Error with parsing coords", username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "error with parsing coords"})
		return
	}

	region_name, region_rus := helpers.DetectPolygonByPoint(*body.Lng, *body.Lat)
	country := os.Getenv("MAIN_COUNTRY")
	country_rus := os.Getenv("MAIN_COUNTRY_RUS")

	region, err := helpers.GetOrCreateRegion(country, country_rus, region_name, region_rus)
	if err != nil {
		helpers.LogError("Error with creating or receiving region", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating or receiving location"})
		return
	}

	updates := models.Camera{
		Login:     body.Login,
		Password:  body.Password,
		Name:      body.Name,
		Lat:       *body.Lat,
		Lng:       *body.Lng,
		Address:   body.Address,
		Comment:   body.Comment,
		IsDefined: true,
		RegionID:  region.ID,
	}

	if body.CityID != nil {
		var city models.City
		if err := initializers.DB.Where("id = ? AND region_id = ?", *body.CityID, region.ID).First(&city).Error; err == nil {
			cid := city.ID
			updates.CityID = &cid
		}
		// If city not found in region, leave city unchanged
	}
	// If body.CityID is nil, preserve existing city — don't overwrite

	if body.MaintainerID != nil {
		updates.MaintainerID = body.MaintainerID
	}

	if err := initializers.DB.
		Model(&models.Camera{}).
		Where("ip = ? AND port = ?", body.IP, body.Port).
		Updates(updates).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Error with updating camera (%s:%s) data", body.IP, body.Port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while updating camera"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Camera (%s:%s) was defined successfully", body.IP, body.Port), username)

	// Fetch updated camera for response
	var definedCam models.Camera
	if err := initializers.DB.Preload("Region").Preload("Region.Country").Preload("MaintainerRef").Preload("CityRef").Where("ip = ? AND port = ?", body.IP, body.Port).First(&definedCam).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{
			"lat": *body.Lat,
			"lng": *body.Lng,
		})
		return
	}

	maintainerName := ""
	if definedCam.MaintainerRef != nil {
		maintainerName = definedCam.MaintainerRef.Name
	}
	c.JSON(http.StatusOK, gin.H{
		"ID":           definedCam.ID,
		"Name":         definedCam.Name,
		"IsDefined":    definedCam.IsDefined,
		"Status":       definedCam.Status,
		"IP":           definedCam.IP,
		"Port":         definedCam.Port,
		"lat":          definedCam.Lat,
		"lng":          definedCam.Lng,
		"Comment":      definedCam.Comment,
		"Link":         definedCam.Link,
		"City":         camCity(definedCam.CityRef),
		"City_rus":     camCityRus(definedCam.CityRef),
		"Region":       definedCam.Region.Name,
		"Region_rus":   definedCam.Region.Name_rus,
		"Country":      definedCam.Region.Country.Name,
		"Country_rus":  definedCam.Region.Country.Name_rus,
		"MaintainerID": definedCam.MaintainerID,
		"Maintainer":   maintainerName,
	})
}

func ChangeStatus(c *gin.Context) {
	var body struct {
		IP     string `json:"ip"`
		Port   string `json:"port"`
		Status string `json:"status"`
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.Bind(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}

	if !helpers.ValidateIP(body.IP) || !helpers.ValidatePort(body.Port) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port are invalid"})
		return
	}

	validStatuses := map[string]bool{"valid": true, "invalid": true, "duplicate": true, "undetectable": true}
	if !validStatuses[body.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status value"})
		return
	}

	var existing models.Camera
	if err := initializers.DB.Select("is_defined").Where("ip = ? AND port = ?", body.IP, body.Port).First(&existing).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}

	updates := map[string]any{"Status": body.Status}
	isDefined := existing.IsDefined
	if body.Status == "invalid" || body.Status == "undetectable" {
		updates["IsDefined"] = false
		isDefined = false
	}

	if err := initializers.DB.
		Model(&models.Camera{}).
		Where("ip = ? AND port = ?", body.IP, body.Port).
		Updates(updates).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Error with updating camera (%s:%s) status in database", body.IP, body.Port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while updating record"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Camera (%s:%s) status was successfully changed", body.IP, body.Port), username)
	c.JSON(http.StatusOK, gin.H{"IsDefined": isDefined})
}

func AddCamera(c *gin.Context) {
	var body struct {
		Name         string   `json:"name"`
		IP           string   `json:"ip"`
		Port         string   `json:"port"`
		Lat          *float64 `json:"lat"`
		Lng          *float64 `json:"lng"`
		Login        string   `json:"login"`
		Password     string   `json:"password"`
		Address      string   `json:"address"`
		Link         string   `json:"link"`
		Comment      string   `json:"comment"`
		Status       string   `json:"status"`
		CityID       *uint    `json:"city_id"`
		MaintainerID *uint    `json:"maintainer_id"`
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.BindJSON(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}

	if body.IP == "" || body.Port == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port can not be empty"})
		return
	}

	if !helpers.ValidateIP(body.IP) || !helpers.ValidatePort(body.Port) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port are invalid"})
		return
	}

	if body.Name == "" {
		body.Name = body.IP
	}

	var count int64
	initializers.DB.Model(&models.Camera{}).Where("ip = ? AND port = ?", body.IP, body.Port).Count(&count)
	if count != 0 {
		helpers.LogError(fmt.Sprintf("Error with adding camera (%s:%s) to database", body.IP, body.Port), username, "camera already exists")
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera with specified ip and port already exists"})
		return
	}

	isDefined := body.Lat != nil && body.Lng != nil
	var lat, lng float64
	var region_name, region_rus, country, country_rus string
	var cameraCityID *uint

	if isDefined {
		lat, lng = *body.Lat, *body.Lng
		country = os.Getenv("MAIN_COUNTRY")
		country_rus = os.Getenv("MAIN_COUNTRY_RUS")
		region_name, region_rus = helpers.DetectPolygonByPoint(lng, lat)
		if region_name == "" {
			helpers.LogError("Could not detect region from coords", username, "")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error while receiving region"})
			return
		}

		// Coords are defined — city and address must be provided
		if body.CityID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "город не выбран"})
			return
		}
		if body.Address == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "адрес не указан"})
			return
		}

		region, err := helpers.GetOrCreateRegion(country, country_rus, region_name, region_rus)
		if err != nil {
			helpers.LogError("Error with creating or receiving region", username, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating or receiving location"})
			return
		}

		var cityRec models.City
		if err := initializers.DB.Where("id = ? AND region_id = ?", *body.CityID, region.ID).First(&cityRec).Error; err != nil {
			helpers.LogError("Selected city not found in region", username, err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": "выбранный город не найден"})
			return
		}
		cityID := cityRec.ID
		cameraCityID = &cityID
	} else {
		// No coords — use IP geolocation
		var city, city_rus string
		location, err := helpers.GetLocation(body.IP)
		if err != nil {
			helpers.LogError(fmt.Sprintf("GetLocation failed for (%s:%s), using fallback", body.IP, body.Port), username, err.Error())
			country = os.Getenv("MAIN_COUNTRY")
			country_rus = os.Getenv("MAIN_COUNTRY_RUS")
			if country == "" {
				country = "Ukraine"
			}
			if country_rus == "" {
				country_rus = "Украина"
			}
			region_name = "Unknown"
			region_rus = "Неизвестно"
			city = "Unknown"
			city_rus = "Неизвестно"
		} else {
			latF, errLat := strconv.ParseFloat(strings.TrimSpace(location.Latitude), 64)
			lngF, errLng := strconv.ParseFloat(strings.TrimSpace(location.Longitude), 64)
			if errLat == nil && errLng == nil {
				lat, lng = latF, lngF
			}
			city = location.City
			city_rus = location.City_rus
			region_name = location.Region
			region_rus = location.Region_rus
			country = location.Country
			country_rus = location.Country_rus

			// Ensure we have minimum required data for region creation
			if country == "" {
				country = os.Getenv("MAIN_COUNTRY")
				country_rus = os.Getenv("MAIN_COUNTRY_RUS")
			}
			if country == "" {
				country = "Ukraine"
				country_rus = "Украина"
			}
			if region_name == "" {
				region_name = "Unknown"
				region_rus = "Неизвестно"
			}

			// Try to find or create a City record so CityID is set
			if city != "" && city != "Unknown" {
				region, _ := helpers.GetOrCreateRegion(country, country_rus, region_name, region_rus)
				cityRec, _, err := helpers.GetOrCreateCity(city, city_rus, region.ID)
				if err == nil && cityRec.ID != 0 {
					cid := cityRec.ID
					cameraCityID = &cid
				}
			}
		}
	}

	region, err := helpers.GetOrCreateRegion(country, country_rus, region_name, region_rus)
	if err != nil {
		helpers.LogError("Error with creating or receiving region", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating or receiving location"})
		return
	}

	status := body.Status
	if status == "" {
		status = "valid"
	}
	validStatuses := map[string]bool{"valid": true, "invalid": true, "duplicate": true, "undetectable": true}
	if !validStatuses[status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status value"})
		return
	}

	camera := models.Camera{
		Name:         body.Name,
		IP:           body.IP,
		Port:         body.Port,
		Login:        body.Login,
		Password:     body.Password,
		Address:      body.Address,
		Comment:      body.Comment,
		Link:         body.Link,
		CityID:       cameraCityID,
		RegionID:     region.ID,
		Lat:          lat,
		Lng:          lng,
		IsDefined:    isDefined,
		Status:       status,
		MaintainerID: body.MaintainerID,
	}

	if err := initializers.DB.Create(&camera).Error; err != nil {
		helpers.LogError("Error while creating camera record", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating camera record"})
		return
	}
	helpers.LogSuccess(fmt.Sprintf("Camera (%s:%s) was successfully added", body.IP, body.Port), username)

	// Fetch region with country for response
	var cameraResp models.Camera
	if err := initializers.DB.Preload("MaintainerRef").Preload("CityRef").First(&cameraResp, camera.ID).Error; err != nil {
		helpers.LogError("Error fetching camera for response", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating camera record"})
		return
	}

	var regionFull models.Region
	if err := initializers.DB.Preload("Country").First(&regionFull, region.ID).Error; err != nil {
		helpers.LogError("Error fetching region for response", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating camera record"})
		return
	}

	maintainerName := ""
	if cameraResp.MaintainerRef != nil {
		maintainerName = cameraResp.MaintainerRef.Name
	}
	c.JSON(http.StatusOK, gin.H{
		"ID":           camera.ID,
		"Name":         camera.Name,
		"IsDefined":    camera.IsDefined,
		"Status":       camera.Status,
		"IP":           camera.IP,
		"Port":         camera.Port,
		"Lat":          camera.Lat,
		"Lng":          camera.Lng,
		"Comment":      camera.Comment,
		"Link":         camera.Link,
		"City":         camCity(cameraResp.CityRef),
		"City_rus":     camCityRus(cameraResp.CityRef),
		"Region":       regionFull.Name,
		"Region_rus":   regionFull.Name_rus,
		"Country":      regionFull.Country.Name,
		"Country_rus":  regionFull.Country.Name_rus,
		"Images":       []string{},
		"MaintainerID": camera.MaintainerID,
		"Maintainer":   maintainerName,
	})
}

func UploadPhotos(c *gin.Context) {
	ip := c.PostForm("ip")
	port := c.PostForm("port")

	_, _, username := middleware.CheckAuth(c)

	if !helpers.ValidateIP(ip) || !helpers.ValidatePort(port) {
		helpers.LogError(fmt.Sprintf("IP or port are invalid (%s:%s)", ip, port), username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port are invalid"})
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		helpers.LogError(fmt.Sprintf("Error with parsign form for camera (%s:%s)", ip, port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid form data"})
		return
	}

	files := form.File["photos"]
	indexes := form.Value["indexes"]

	if len(files) == 0 || len(files) != len(indexes) {
		helpers.LogError(fmt.Sprintf("Files does not provided or count of indexes does not satisfied count of files for camera (%s:%s)", ip, port), username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "incorrect form data provided"})
		return
	}

	saveDir := fmt.Sprintf("./data/photos/%s", ip)
	index, err := helpers.GetLastPhotoIndex("./data/photos", ip, port)
	if err != nil {
		helpers.LogError(fmt.Sprintf("Error receive last photo index for camera (%s:%s)", ip, port), username, "")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "can not get last index"})
		return
	}

	if err := os.MkdirAll(saveDir, 0755); err != nil {
		helpers.LogError(fmt.Sprintf("Error creating photo directory for camera (%s:%s)", ip, port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error creating storage directory"})
		return
	}

	type result struct {
		Index    int    `json:"index"`
		Success  bool   `json:"success"`
		Filename string `json:"filename"`
	}
	var savedFiles []result

	for i, file := range files {
		fileIndex, _ := strconv.Atoi(indexes[i])
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			savedFiles = append(savedFiles, result{Index: fileIndex, Success: false})
			continue
		}

		filename := fmt.Sprintf("%s_%s_%d.jpg", ip, port, index)
		savePath := filepath.Join(saveDir, filename)
		if err := c.SaveUploadedFile(file, savePath); err != nil {
			savedFiles = append(savedFiles, result{Index: fileIndex, Success: false})
			continue
		}

		savedFiles = append(savedFiles, result{
			Index:    fileIndex,
			Success:  true,
			Filename: filename,
		})
		index++
	}

	helpers.LogSuccess(fmt.Sprintf("Photos for camera (%s:%s) were successfully uploaded", ip, port), username)
	c.JSON(http.StatusOK, savedFiles)
}

func DeletePhoto(c *gin.Context) {
	var body struct {
		IP       string `json:"ip"`
		Port     string `json:"port"`
		Filename string `json:"filename"`
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		helpers.LogError("Error while binding body", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while bind body"})
		return
	}

	if !helpers.ValidateIP(body.IP) || !helpers.ValidatePort(body.Port) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ip or port"})
		return
	}

	camDir := fmt.Sprintf("./data/photos/%s", body.IP)
	filePath := filepath.Join(camDir, body.Filename)

	expectedPrefix := fmt.Sprintf("%s_%s", body.IP, body.Port)
	if !strings.HasPrefix(body.Filename, expectedPrefix) || !strings.HasSuffix(strings.ToLower(body.Filename), ".jpg") {
		helpers.LogError("Attempt to delete unauthorized file", username, body.Filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file not found"})
			return
		}
		helpers.LogError("Error deleting photo", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error deleting file"})
		return
	}

	entries, err := os.ReadDir(camDir)
	if err == nil && len(entries) == 0 {
		if err := os.Remove(camDir); err != nil {
			helpers.LogError("Error removing camera directory", username, err.Error())
		}
	}

	helpers.LogSuccess(fmt.Sprintf("Photo %s deleted successfully", body.Filename), username)
	c.JSON(http.StatusOK, gin.H{})
}

// GET /geo/search?q=...
// Proxy to Nominatim for address geocoding
func GeoSearch(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing query"})
		return
	}

	nominatimURL := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=10&addressdetails=1&accept-language=ru",
		url.QueryEscape(query))

	req, err := http.NewRequest("GET", nominatimURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}
	req.Header.Set("User-Agent", "Samar/1.0")
	req.Header.Set("Accept-Language", "ru")

	nominatimClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := nominatimClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "nominatim request failed"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": "nominatim returned " + resp.Status})
		return
	}

	// Nominatim returns: [{ "display_name": "...", "lat": "...", "lon": "...", ... }, ...]
	// Forward as-is — frontend expects exactly these fields
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read response"})
		return
	}

	c.Data(http.StatusOK, "application/json", body)
}

// GET /cam/cities?region_id=N
func GetCities(c *gin.Context) {
	type CityDTO struct {
		ID            uint   `json:"ID"`
		Name          string `json:"Name"`
		Name_rus      string `json:"Name_rus"`
		RegionID      uint   `json:"RegionID"`
		RegionName    string `json:"RegionName"`
		RegionNameRus string `json:"RegionNameRus"`
	}

	q := initializers.DB.
		Model(&models.City{}).
		Select("cities.id, cities.name, cities.name_rus, cities.region_id, regions.name as region_name, regions.name_rus as region_name_rus").
		Joins("LEFT JOIN regions ON regions.id = cities.region_id").
		Order("cities.name")

	if regionID := c.Query("region_id"); regionID != "" {
		q = q.Where("cities.region_id = ?", regionID)
	}

	var cities []CityDTO
	if err := q.Scan(&cities).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if cities == nil {
		cities = []CityDTO{}
	}
	c.JSON(http.StatusOK, cities)
}

// POST /cam/add_city
func AddCity(c *gin.Context) {
	var body struct {
		Name     string `json:"name"`
		NameRus  string `json:"name_rus"`
		RegionID uint   `json:"region_id"`
	}
	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if body.RegionID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "region_id is required"})
		return
	}
	cityName := body.Name
	if cityName == "" {
		cityName = body.NameRus
	}
	city, _, err := helpers.GetOrCreateCity(cityName, body.NameRus, body.RegionID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return DTO with region names for the frontend city picker
	type CityDTO struct {
		ID            uint   `json:"ID"`
		Name          string `json:"Name"`
		Name_rus      string `json:"Name_rus"`
		RegionID      uint   `json:"RegionID"`
		RegionName    string `json:"RegionName"`
		RegionNameRus string `json:"RegionNameRus"`
	}
	dto := CityDTO{
		ID:       city.ID,
		Name:     city.Name,
		Name_rus: city.Name_rus,
		RegionID: city.RegionID,
	}
	if city.RegionID > 0 {
		var region models.Region
		if err := initializers.DB.First(&region, city.RegionID).Error; err == nil {
			dto.RegionName = region.Name
			dto.RegionNameRus = region.Name_rus
		}
	}
	c.JSON(http.StatusOK, dto)
}

// GET /cam/maintainers
func GetMaintainers(c *gin.Context) {
	var maintainers []models.Maintainer
	if err := initializers.DB.Order("name").Find(&maintainers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, maintainers)
}

// POST /cam/add_maintainer
func AddMaintainer(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindBodyWithJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	m := models.Maintainer{Name: strings.TrimSpace(body.Name)}
	if err := initializers.DB.FirstOrCreate(&m, models.Maintainer{Name: m.Name}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	c.JSON(http.StatusOK, m)
}

// GET /cam/region?lat=N&lng=N
func GetRegionByCoords(c *gin.Context) {
	lat, errLat := strconv.ParseFloat(c.Query("lat"), 64)
	lng, errLng := strconv.ParseFloat(c.Query("lng"), 64)
	if errLat != nil || errLng != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid coordinates"})
		return
	}

	regionName, regionNameRus := helpers.DetectPolygonByPoint(lng, lat)

	country := os.Getenv("MAIN_COUNTRY")
	countryRus := os.Getenv("MAIN_COUNTRY_RUS")
	region, err := helpers.GetOrCreateRegion(country, countryRus, regionName, regionNameRus)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error resolving region"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":       region.ID,
		"name":     region.Name,
		"name_rus": region.Name_rus,
	})
}

// GET /cam/region_by_ip?ip=X
func GetRegionByIP(c *gin.Context) {
	ip := strings.TrimSpace(c.Query("ip"))
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip required"})
		return
	}

	location, err := helpers.GetLocation(ip)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"id": 0, "name": "", "name_rus": ""})
		return
	}

	country := os.Getenv("MAIN_COUNTRY")
	countryRus := os.Getenv("MAIN_COUNTRY_RUS")
	region, err := helpers.GetOrCreateRegion(country, countryRus, location.Region, location.Region_rus)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error resolving region"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":       region.ID,
		"name":     region.Name,
		"name_rus": region.Name_rus,
	})
}

// DELETE /cam/delete_cam
func DeleteCamera(c *gin.Context) {
	var body struct {
		IP   string `json:"ip"`
		Port string `json:"port"`
	}
	_, _, username := middleware.CheckAuth(c)
	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if body.IP == "" || body.Port == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip and port required"})
		return
	}

	if !helpers.ValidateIP(body.IP) || !helpers.ValidatePort(body.Port) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip or port are invalid"})
		return
	}

	var cam models.Camera
	if err := initializers.DB.Where("ip = ? AND port = ?", body.IP, body.Port).First(&cam).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}

	// Delete only photos that belong to this specific camera (ip+port).
	// Other cameras sharing the same IP (different port) must not be affected.
	photoDir := fmt.Sprintf("./data/photos/%s", cam.IP)
	prefix := fmt.Sprintf("%s_%s_", cam.IP, cam.Port)
	if entries, err := os.ReadDir(photoDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
				os.Remove(filepath.Join(photoDir, entry.Name()))
			}
		}
		// Remove the directory only if it is now empty
		if remaining, err := os.ReadDir(photoDir); err == nil && len(remaining) == 0 {
			os.Remove(photoDir)
		}
	}

	if err := initializers.DB.Unscoped().Delete(&cam).Error; err != nil {
		helpers.LogError("Error deleting camera", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Camera (%s:%s) deleted", cam.IP, cam.Port), username)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
