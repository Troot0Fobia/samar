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
		isAuth, userRole, _ := CheckAuth(c)
		userRoleIndex := slices.Index(userRoles, userRole)
		if userRoleIndex < role {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if isAuth && method == http.MethodGet && path == "/auth" {
			c.Redirect(http.StatusFound, "/")
			c.Abort()
			return
		}

		if !isAuth && method == http.MethodGet {
			if path == "/" {
				c.Redirect(http.StatusFound, "/auth")
				c.Abort()
				return
			}
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"redirect": true})
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

func GetHomePage(c *gin.Context) {
	_, role, _ := CheckAuth(c)
	c.HTML(http.StatusOK, "map.html", gin.H{"isAdmin": role == "admin", "qeModer": role == "admin" || role == "moderator"})
}
