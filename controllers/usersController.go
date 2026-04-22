package controllers

import (
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func Signup(c *gin.Context) {
	var body struct {
		Username string
		Token    string
	}

	if isAuth, _, _ := middleware.CheckAuth(c); isAuth {
		c.Abort()
		return
	}

	if err := c.Bind(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", "guest", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}
	isValid, role := helpers.ValidateInviteToken(body.Token)

	if !isValid {
		helpers.LogError(fmt.Sprintf("Invalid token was provided: %s", body.Token), body.Username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is invalid"})
		return
	}

	if role == "admin" {
		helpers.LogError("Attempt to register admin via invite token", body.Username, "")
		c.JSON(http.StatusForbidden, gin.H{"error": "admin registration via token is not allowed"})
		return
	}

	var exists bool
	if err := initializers.DB.Model(&models.User{}).
		Select("count(*) > 0").
		Where("username = ?", body.Username).
		Find(&exists).Error; err != nil {
		helpers.LogError("Error check username", body.Username, "")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error check username"})
		return
	}

	if exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username already exists"})
		return
	}

	password := helpers.GeneratePassword(12)
	passHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		helpers.LogError("Error hashing password", body.Username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if err := initializers.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&models.User{Username: body.Username, Role: role, PassHash: string(passHash)}).Error; err != nil {
			return err
		}
		return tx.Model(&models.InviteToken{}).Where("token = ?", body.Token).Update("used", true).Error
	}); err != nil {
		helpers.LogError("Error creating user", body.Username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error creating user"})
		return
	}

	helpers.LogSuccess("User was successfully created", body.Username)
	c.JSON(http.StatusOK, gin.H{
		"username": body.Username,
		"password": password,
	})
}

func Login(c *gin.Context) {
	var body struct {
		Username string
		Password string
	}

	if isAuth, _, _ := middleware.CheckAuth(c); isAuth {
		c.Abort()
		return
	}

	if err := c.Bind(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", "guest", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "error while bind request"})
		return
	}

	var user models.User
	if err := initializers.DB.First(&user, "username = ?", body.Username).Error; err != nil {
		helpers.LogError("Wrong username for access to account", body.Username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid username or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(body.Password)); err != nil {
		helpers.LogError("Wrong password for access to account", body.Username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid username or password"})
		return
	}

	sessionToken := helpers.GenerateToken(32)
	csrfToken := helpers.GenerateToken(32)

	if err := initializers.DB.Create(&models.Session{
		UserID:    user.ID,
		TokenHash: helpers.HashToken(sessionToken),
		CSRFToken: csrfToken,
		Expires:   time.Now().Add(24 * time.Hour),
	}).Error; err != nil {
		helpers.LogError("Error creating session", body.Username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.SetSameSite(http.SameSiteLaxMode)

	c.SetCookie("csrf_token", csrfToken, 24*3600, "/", "", !initializers.IsDevelopment, false)
	c.SetCookie("access_token", sessionToken, 24*3600, "/", "", !initializers.IsDevelopment, true)

	helpers.LogSuccess("User was successfully logged in", body.Username)
	c.JSON(http.StatusOK, gin.H{})
}

func Logout(c *gin.Context) {
	if isAuth, _, _ := middleware.CheckAuth(c); isAuth {
		access_token, _ := c.Cookie("access_token")
		csrf_token, _ := c.Cookie("csrf_token")

		c.SetCookie("csrf_token", "", -1, "/", "", !initializers.IsDevelopment, false)
		c.SetCookie("access_token", "", -1, "/", "", !initializers.IsDevelopment, true)
		if err := initializers.DB.Model(&models.Session{}).Where("token_hash = ? AND csrf_token = ?", helpers.HashToken(access_token), csrf_token).Update("active", false).Error; err != nil {
			helpers.LogError("Error deactivating session on logout", "", err.Error())
		}

		c.JSON(http.StatusOK, gin.H{})
		return
	}
	c.AbortWithStatus(http.StatusBadRequest)
}

func RefreshToken(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}


func GetRegisterToken(c *gin.Context) {
	var body struct {
		Role string
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.BindJSON(&body); err != nil {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect body or invalid role"})
		return
	}
	if body.Role == "" {
		helpers.LogError("Empty role in get_token request", username, "")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect body or invalid role"})
		return
	}

	if body.Role == "admin" {
		helpers.LogError("Attempt to generate admin invite token", username, "")
		c.JSON(http.StatusForbidden, gin.H{"error": "admin tokens are not allowed; use CLI to create admins"})
		return
	}

	token, err := helpers.CreateInviteToken(body.Role)
	if err != nil {
		helpers.LogError("Error creating invite token", username, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error creating token"})
		return
	}

	helpers.LogSuccess("Successfully received registration token", username)
	c.JSON(http.StatusOK, gin.H{"token": token})
}
