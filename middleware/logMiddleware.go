package middleware

import (
	"Troot0Fobia/samar/initializers"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func RequestLog(c *gin.Context) {
	start := time.Now()

	c.Next()

	isAuth, role, username := CheckAuth(c)
	if !isAuth {
		role = "guest"
		username = "guest"
	}
	initializers.InfoLog.WithFields(logrus.Fields{
		"method":    c.Request.Method,
		"auth":      isAuth,
		"role":      role,
		"username":  username,
		"path":      c.Request.URL.Path,
		"status":    c.Writer.Status(),
		"latency":   time.Since(start).String(),
		"userAgent": c.Request.UserAgent(),
	}).Info("HTTP Request")
}
