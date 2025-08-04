package controllers

import (
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func Signup(c *gin.Context) {
	var body struct {
		Username string
		Token    string
	}

	if c.Bind(&body) != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed ready body",
		})
		return
	}
	isValid, role := helpers.ValidateInviteToken(body.Token)

	if !isValid {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Token is invalid",
		})
		return
	}

	var exists bool
	err := initializers.DB.Model(&models.User{}).
		Select("count(*) > 0").
		Where("username = ?", body.Username).
		Find(&exists).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Error check username",
		})
		return
	}

	if exists {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Username already exists",
		})
		return
	}

	password := helpers.GeneratePassword(12)
	passHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	initializers.DB.Create(&models.User{Username: body.Username, Role: role, PassHash: string(passHash)})
	initializers.DB.Model(&models.InviteToken{}).Where("token = ?", body.Token).Update("used", true)

	c.JSON(http.StatusOK, gin.H{
		"username": body.Username,
		"password": password,
	})
}

func Login(c *gin.Context) {
	var creds struct {
		Username string
		Password string
	}

	if c.Bind(&creds) != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed read the body",
		})
		return
	}

	var user models.User
	initializers.DB.First(&user, "username = ?", creds.Username)

	if user.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid username or password",
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(creds.Password)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid username or password while compairing",
		})
		return
	}

	sessionToken := helpers.GenerateToken(32)
	csrfToken := helpers.GenerateToken(32)

	initializers.DB.Create(&models.Session{
		UserID:    user.ID,
		TokenHash: helpers.HashToken(sessionToken),
		CSRFToken: csrfToken,
		Expires:   time.Now().Add(24 * time.Hour),
	})

	c.SetSameSite(http.SameSiteLaxMode)

	c.SetCookie("csrf_token", csrfToken, 3600, "/", "", false, false)
	c.SetCookie("access_token", sessionToken, 3600, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

func Logout(c *gin.Context) {
	if isAuth, _ := middleware.CheckAuth(c); isAuth {
		access_token, _ := c.Cookie("access_token")
		csrf_token, _ := c.Cookie("csrf_token")

		c.SetCookie("csrf_token", "", -1, "/", "", false, false)
		c.SetCookie("access_token", "", -1, "/", "", false, true)
		initializers.DB.Model(&models.Session{}).Where("token_hash = ? AND csrf_token = ?", helpers.HashToken(access_token), csrf_token).Update("active", false)

		c.JSON(http.StatusOK, gin.H{})
		return
	}
	c.AbortWithStatus(http.StatusBadRequest)
}

func NoCahceHTML(c *gin.Context) {
	c.Writer.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Writer.Header().Set("Pragma", "no-cache")
	c.Writer.Header().Set("Expires", "0")
	c.Next()
}

func GetRegisterToken(c *gin.Context) {
	var body struct {
		Role string `json:"role"`
	}

	if err := c.BindJSON(&body); err != nil || body.Role == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role"})
		return
	}

	token := helpers.CreateInviteToken(body.Role)
	c.JSON(http.StatusOK, gin.H{"token": token})
}
