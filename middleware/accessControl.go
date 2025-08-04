package middleware

import (
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func AccessControl() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method
		isAuth, role := CheckAuth(c)
		fmt.Printf("If user authorized: %t\n", isAuth)
		fmt.Printf("Path for access: %v\n", path)

		publicPaths := map[string]bool{
			"/auth":          true,
			"/auth/login":    true,
			"/auth/register": true,
		}

		if isAuth {
			if path == "/auth" && method == http.MethodGet {
				c.Redirect(http.StatusFound, "/")
				c.Abort()
				return
			}
			if (path == "/auth/login" || path == "/auth/register") && method == http.MethodPost {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			if strings.HasPrefix(path, "/admin") && role != "admin" {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
			c.Next()
			return
		}

		if publicPaths[path] {
			c.Next()
			return
		}
		if method == http.MethodGet {
			if path == "/" {
				c.Redirect(http.StatusFound, "/auth")
				c.Abort()
				return
			}
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"redirect": true})
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
	}
}

func CheckAuth(c *gin.Context) (bool, string) {
	sessionToken, err := c.Cookie("access_token")
	if err != nil || sessionToken == "" {
		return false, ""
	}

	csrfToken, err := c.Cookie("csrf_token")
	if err != nil || csrfToken == "" {
		return false, ""
	}

	if c.Request.Method != http.MethodGet {
		csrfHeader := c.GetHeader("X-CSRF-Token")
		if csrfHeader != csrfToken {
			return false, ""
		}
	}

	var session models.Session
	err = initializers.DB.
		Preload("User").
		First(&session, "token_hash = ? AND csrf_token = ?", helpers.HashToken(sessionToken), csrfToken).
		Error
	if err != nil || time.Now().After(session.Expires) || !session.Active {
		return false, ""
	}

	return true, session.User.Role
}
