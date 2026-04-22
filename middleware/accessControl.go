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

		// HTTP 404 instead of 401/403 is intentional: obscures the existence of
		// protected endpoints from unauthenticated scanners.
		if userRoleIndex := slices.Index(userRoles, userRole); userRoleIndex < role {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if isAuth {
			if err := refreshSessionToken(c); err != nil {
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

	c.Set("userID", session.UserID)
	c.Set("session", session)

	return true, session.User.Role, session.User.Username
}

func refreshSessionToken(c *gin.Context) error {
	const refreshInterval = time.Hour

	val, exists := c.Get("session")
	if !exists {
		return nil
	}
	session, ok := val.(models.Session)
	if !ok {
		return nil
	}

	// Only write to DB if expiry moved by more than refreshInterval to avoid hammering SQLite on every request.
	newExpiry := time.Now().Add(24 * time.Hour)
	if session.Expires.Sub(newExpiry) > -refreshInterval {
		// Still refresh the cookie max-age on every request so the browser never drops it.
		sessionToken, _ := c.Cookie("access_token")
		csrfToken, _ := c.Cookie("csrf_token")
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("csrf_token", csrfToken, 24*3600, "/", "", !initializers.IsDevelopment, false)
		c.SetCookie("access_token", sessionToken, 24*3600, "/", "", !initializers.IsDevelopment, true)
		return nil
	}

	session.Expires = newExpiry
	if err := initializers.DB.Model(&session).Update("expires", session.Expires).Error; err != nil {
		return err
	}

	sessionToken, _ := c.Cookie("access_token")
	csrfToken, _ := c.Cookie("csrf_token")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("csrf_token", csrfToken, 24*3600, "/", "", !initializers.IsDevelopment, false)
	c.SetCookie("access_token", sessionToken, 24*3600, "/", "", !initializers.IsDevelopment, true)
	return nil
}

func GetHomePage(c *gin.Context) {
	_, role, _ := CheckAuth(c)
	c.HTML(http.StatusOK, "map.html", gin.H{"isAdmin": role == "admin", "isModer": role == "admin" || role == "moderator"})
}
