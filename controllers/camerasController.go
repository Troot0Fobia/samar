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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	Vuln        string  `json:"vuln"`
}

func GetCams(c *gin.Context) {
	var cams []models.Camera
	_, role, username := middleware.CheckAuth(c)
	if err := initializers.DB.
		Preload("Region").
		Preload("Region.Country").
		Order("city, ip").
		Find(&cams).Error; err != nil {
		helpers.LogError("Error with receiving cams from database", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	type SendCamera struct {
		ID          uint
		Name        string
		IsDefined   bool
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
		if role == "user" && (cam.Status == "invalid" || cam.Status == "duplicate") {
			continue
		}
		var images []string
		entries, err := os.ReadDir(fmt.Sprintf("./data/photos/%s", cam.IP))
		if err == nil {
			prefix := fmt.Sprintf("%s_%s", cam.IP, cam.Port)
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				filename := entry.Name()
				if strings.HasPrefix(filename, prefix) && strings.HasSuffix(strings.ToLower(filename), ".jpg") {
					images = append(images, url.QueryEscape(filename))
				}
			}
		}
		result = append(result, SendCamera{
			ID:          cam.ID,
			Name:        cam.Name,
			IsDefined:   cam.IsDefined,
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

	_, _, username := middleware.CheckAuth(c)

	var camera models.Camera
	if err := initializers.DB.
		Select("name, ip, port, login, password, status, address, lat, lng, comment").
		Where("ip = ? AND port = ?", ip, port).
		Find(&camera).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Error with receiving cam info (%s:%s) from database", ip, port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while get cam info"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"IP":       camera.IP,
		"Port":     camera.Port,
		"Name":     camera.Name,
		"Login":    camera.Login,
		"Password": camera.Password,
		"Lat":      camera.Lat,
		"Lng":      camera.Lng,
		"Comment":  camera.Comment,
		"Address":  camera.Address,
		"Status":   camera.Status,
	})
}

func SaveComment(c *gin.Context) {
	var body struct {
		Ip      string
		Port    string
		Comment string
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.Bind(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while bind request"})
		return
	}

	if err := initializers.DB.Model(&models.Camera{}).Where("ip = ? AND port = ?", body.Ip, body.Port).Update("comment", body.Comment).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Error with updating cam (%s:%s) comment", body.Ip, body.Port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error while updating data"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Comment for camera (%s:%s) changed successfully", body.Ip, body.Port), username)
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
	_, _, username := middleware.CheckAuth(c)

	if !strings.HasSuffix(file.Filename, ".json") {
		helpers.LogError("File does not satisfied established format", username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file"})
		return
	}

	file_data, _ := file.Open()
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

	for _, record := range cam_data {
		region, err := getOrCreateRegion(record.Country, record.Country_rus, record.Region, record.Region_rus)
		if err != nil {
			continue
		}
		camera := models.Camera{
			IP:            record.Ip,
			Port:          record.Port,
			Login:         record.Login,
			Password:      record.Password,
			Channels:      record.Channels,
			City:          record.City,
			City_rus:      record.City_rus,
			RegionID:      region.ID,
			Lat:           record.Lat,
			Lng:           record.Lng,
			Vulnerability: record.Vuln,
			IsDefined:     false,
			Status:        "valid",
		}

		var existing models.Camera
		result := initializers.DB.Where("ip = ? AND port = ?", camera.IP, camera.Port).First(&existing)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			initializers.DB.Create(&camera)
		}
	}
	helpers.LogSuccess("Cameras were uploaded successfully", username)
	c.JSON(http.StatusOK, gin.H{})
}

func GetCamImage(c *gin.Context) {
	ip := c.Param("ip")
	image := c.Param("image")
	ip = strings.ReplaceAll(ip, "../", "")
	image = strings.ReplaceAll(image, "../", "")

	c.File(fmt.Sprintf("./data/photos/%s/%s", ip, image))
}

func DefineCam(c *gin.Context) {
	var body struct {
		IP       string
		Port     string
		Login    string
		Password string
		Name     string
		Coords   string
		Address  string
		Comment  string
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.Bind(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}

	lat, lng, err := helpers.ParseCoords(body.Coords)
	if err != nil {
		helpers.LogError("Error with parsing coords", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error with parsing coords"})
		return
	}

	region_name, region_rus := helpers.DetectPolygonByPoint(lng, lat)
	city := "Unknown"
	city_rus := "Неизвестно"
	country := os.Getenv("MAIN_COUNTRY")
	country_rus := os.Getenv("MAIN_COUNTRY_RUS")

	region, err := getOrCreateRegion(country, country_rus, region_name, region_rus)
	if err != nil {
		helpers.LogError("Error with creating or receiving region", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating or receiving location"})
		return
	}

	if err := initializers.DB.
		Model(&models.Camera{}).
		Where("ip = ? AND port = ?", body.IP, body.Port).
		Updates(models.Camera{
			Login:     body.Login,
			Password:  body.Password,
			Name:      body.Name,
			Lat:       lat,
			Lng:       lng,
			Address:   body.Address,
			Comment:   body.Comment,
			IsDefined: true,
			City:      city,
			City_rus:  city_rus,
			RegionID:  region.ID,
		}).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Error with updating camera (%s:%s) data", body.IP, body.Port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while updating camera"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Camera (%s:%s) was defined successfully", body.IP, body.Port), username)
	c.JSON(http.StatusOK, gin.H{})
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

	if err := initializers.DB.
		Model(&models.Camera{}).
		Where("ip = ? AND port = ?", body.IP, body.Port).
		Updates(models.Camera{Status: body.Status}).Error; err != nil {
		helpers.LogError(fmt.Sprintf("Error with updating camera (%s:%s) status in database", body.IP, body.Port), username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while updating record"})
		return
	}

	helpers.LogSuccess(fmt.Sprintf("Camera (%s:%s) status was successfully changed", body.IP, body.Port), username)
	c.JSON(http.StatusOK, gin.H{})
}

func AddCamera(c *gin.Context) {
	var body struct {
		Name     string `json:"name"`
		IP       string `json:"ip"`
		Port     string `json:"port"`
		Coords   string `json:"coords"`
		Login    string `json:"login"`
		Password string `json:"password"`
		Address  string `json:"address"`
		Comment  string `json:"comment"`
		Status   string `json:"status"`
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.BindJSON(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}

	isDefined := body.Coords != ""
	var coords, region_name, region_rus, country, country_rus, city, city_rus string

	if isDefined {
		coords = body.Coords
		city = "Unknown"
		city_rus = "Неизвестно"
		country = os.Getenv("MAIN_COUNTRY")
		country_rus = os.Getenv("MAIN_COUNTRY_RUS")
	} else {
		location, err := helpers.GetLocation(body.IP)
		if err != nil {
			helpers.LogError(fmt.Sprintf("Error with receiving location for camera (%s:%s)", body.IP, body.Port), username, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error while defining camera location"})
			return
		}
		coords = strings.Join([]string{location.Latitude, location.Longitude}, ", ")
		city = location.City
		city_rus = location.City_rus
		region_name = location.Region
		region_rus = location.Region_rus
		country = location.Country
		country_rus = location.Country_rus
	}

	lat, lng, err := helpers.ParseCoords(coords)
	if err != nil {
		helpers.LogError("Error with parsing coords", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error with parsing coords"})
		return
	}

	if isDefined {
		if region_name, region_rus = helpers.DetectPolygonByPoint(lng, lat); region_name == "" || region_rus == "" {
			helpers.LogError("Error with receiving region from coords", username, "")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error while receiving region"})
			return
		}
	}

	region, err := getOrCreateRegion(country, country_rus, region_name, region_rus)
	if err != nil {
		helpers.LogError("Error with creating or receiving region", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error while creating or receiving location"})
		return
	}

	camera := models.Camera{
		Name:      body.Name,
		IP:        body.IP,
		Port:      body.Port,
		Login:     body.Login,
		Password:  body.Password,
		Address:   body.Address,
		City:      city,
		City_rus:  city_rus,
		RegionID:  region.ID,
		Lat:       lat,
		Lng:       lng,
		IsDefined: isDefined,
		Status:    body.Status,
	}

	var existing models.Camera
	err = initializers.DB.Where("ip = ? AND port = ?", camera.IP, camera.Port).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		initializers.DB.Create(&camera)
		helpers.LogSuccess(fmt.Sprintf("Camera (%s:%s) was successfully added", body.IP, body.Port), username)

		var images []string
		entries, err := os.ReadDir(fmt.Sprintf("./data/photos/%s", camera.IP))
		if err == nil {
			prefix := fmt.Sprintf("%s_%s", camera.IP, camera.Port)
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				filename := entry.Name()
				if strings.HasPrefix(filename, prefix) && strings.HasSuffix(strings.ToLower(filename), ".jpg") {
					images = append(images, url.QueryEscape(filename))
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"ID":          camera.ID,
			"Name":        camera.Name,
			"IsDefined":   camera.IsDefined,
			"Status":      camera.Status,
			"IP":          camera.IP,
			"Port":        camera.Port,
			"Lat":         camera.Lat,
			"Lng":         camera.Lng,
			"Comment":     camera.Comment,
			"City":        camera.City,
			"City_rus":    camera.City_rus,
			"Region":      region_name,
			"Region_rus":  region_rus,
			"Country":     country,
			"Country_rus": country_rus,
			"Images":      images,
		})
		return
	}
	helpers.LogError(fmt.Sprintf("Error with adding camera (%s:%s) to database", body.IP, body.Port), username, "camera already exists")
	c.JSON(http.StatusInternalServerError, gin.H{"error": "camera with specified values ip and port already exists"})
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

	if index == 1 {
		os.MkdirAll(saveDir, 0755)
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

	camDir := fmt.Sprintf("./data/photos/%s", body.IP)
	filePath := filepath.Join(camDir, body.Filename)

	excpectedPrefix := fmt.Sprintf("%s_%s", body.IP, body.Port)
	if !strings.HasPrefix(body.Filename, excpectedPrefix) || !strings.HasSuffix(strings.ToLower(body.Filename), ".jpg") {
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
