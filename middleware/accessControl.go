package middleware

import (
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	RoleGuest int = iota
	RoleUser
	RoleModer
	RoleAdmin
)

func RequireRole(role int) gin.HandlerFunc {
	userRoles := []string{"", "user", "moderator", "admin"}
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method
		isAuth, userRole, username := CheckAuth(c)

		if !isAuth && method == http.MethodGet {
			if path == "/" {
				c.Redirect(http.StatusFound, "/auth")
				c.Abort()
				return
			}
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"redirect": true})
			return
		}

		if userRoleIndex := slices.Index(userRoles, userRole); userRoleIndex < role {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if isAuth {
			if err := refreshSessionToken(c, username); err != nil {
				helpers.LogError("Error while updating session token for user", username, err.Error())
			}
		}

		if isAuth && method == http.MethodGet && path == "/auth" {
			c.Redirect(http.StatusFound, "/")
			c.Abort()
			return
		}

		c.Next()
	}
}

func CheckAuth(c *gin.Context) (bool, string, string) {
	sessionToken, err := c.Cookie("access_token")
	if err != nil || sessionToken == "" {
		return false, "", ""
	}

	csrfToken, err := c.Cookie("csrf_token")
	if err != nil || csrfToken == "" {
		return false, "", ""
	}

	if c.Request.Method != http.MethodGet {
		csrfHeader := c.GetHeader("X-CSRF-Token")
		if csrfHeader != csrfToken {
			return false, "", ""
		}
	}

	var session models.Session
	err = initializers.DB.
		Preload("User").
		First(&session, "token_hash = ? AND csrf_token = ?", helpers.HashToken(sessionToken), csrfToken).
		Error
	if err != nil || time.Now().After(session.Expires) || !session.Active {
		return false, "", ""
	}

	return true, session.User.Role, session.User.Username
}

func refreshSessionToken(c *gin.Context, username string) error {
	const refreshTHreshold = 10 * time.Minute

	var session models.Session
	err := initializers.DB.
		Joins("User").
		Where("User.username = ?", username).
		First(&session).Error
	if err != nil {
		return err
	}

	timeLeft := time.Until(session.Expires)
	if timeLeft <= refreshTHreshold {
		newExpire := time.Now().Add(1 * time.Hour)
		session.Expires = newExpire
		if err := initializers.DB.Save(&session).Error; err != nil {
			return err
		}

		csrfToken, err := c.Cookie("csrf_token")
		if err != nil || csrfToken == "" {
			return err
		}

		sessionToken, err := c.Cookie("access_token")
		if err != nil || sessionToken == "" {
			return err
		}

		c.SetSameSite(http.SameSiteLaxMode)

		c.SetCookie("csrf_token", csrfToken, 3600, "/", "", true, false)
		c.SetCookie("access_token", sessionToken, 3600, "/", "", true, true)
	}

	return nil
}

func GetHomePage(c *gin.Context) {
	_, role, _ := CheckAuth(c)
	c.HTML(http.StatusOK, "map.html", gin.H{"isAdmin": role == "admin", "qeModer": role == "admin" || role == "moderator"})
}
