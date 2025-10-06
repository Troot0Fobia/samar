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
	passHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	initializers.DB.Create(&models.User{Username: body.Username, Role: role, PassHash: string(passHash)})
	initializers.DB.Model(&models.InviteToken{}).Where("token = ?", body.Token).Update("used", true)

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
	initializers.DB.First(&user, "username = ?", body.Username)

	if user.ID == 0 {
		helpers.LogError("Wrong username for access to account", body.Username, "")
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

	initializers.DB.Create(&models.Session{
		UserID:    user.ID,
		TokenHash: helpers.HashToken(sessionToken),
		CSRFToken: csrfToken,
		Expires:   time.Now().Add(24 * time.Hour),
	})

	c.SetSameSite(http.SameSiteLaxMode)

	c.SetCookie("csrf_token", csrfToken, 3600, "/", "", true, false)
	c.SetCookie("access_token", sessionToken, 3600, "/", "", true, true)

	helpers.LogSuccess("User was successfully logged in", body.Username)
	c.JSON(http.StatusOK, gin.H{})
}

func Logout(c *gin.Context) {
	if isAuth, _, _ := middleware.CheckAuth(c); isAuth {
		access_token, _ := c.Cookie("access_token")
		csrf_token, _ := c.Cookie("csrf_token")

		c.SetCookie("csrf_token", "", -1, "/", "", true, false)
		c.SetCookie("access_token", "", -1, "/", "", true, true)
		initializers.DB.Model(&models.Session{}).Where("token_hash = ? AND csrf_token = ?", helpers.HashToken(access_token), csrf_token).Update("active", false)

		c.JSON(http.StatusOK, gin.H{})
		return
	}
	c.AbortWithStatus(http.StatusBadRequest)
}

func RefreshToken(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// func NoCahceHTML(c *gin.Context) {
// 	c.Writer.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
// 	c.Writer.Header().Set("Pragma", "no-cache")
// 	c.Writer.Header().Set("Expires", "0")
// 	c.Next()
// }

func GetRegisterToken(c *gin.Context) {
	var body struct {
		Role string
	}

	_, _, username := middleware.CheckAuth(c)

	if err := c.BindJSON(&body); err != nil || body.Role == "" {
		helpers.LogError("Error with binding request body to structure", username, err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect body or invalid role"})
		return
	}

	token := helpers.CreateInviteToken(body.Role)

	helpers.LogSuccess("Successfully received registration token", username)
	c.JSON(http.StatusOK, gin.H{"token": token})
}
