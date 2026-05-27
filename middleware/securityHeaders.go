package middleware

import (
	"Troot0Fobia/samar/initializers"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders sets security-related HTTP response headers.
// Must be registered before router.StaticFile() calls.
func SecurityHeaders(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
	c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

	// TODO: replace 'unsafe-inline' in script-src with a nonce-based CSP
	// (requires per-request nonce injected into templates and all inline scripts).
	csp := "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' https://unpkg.com https://cdn.jsdelivr.net https://cdnjs.cloudflare.com; " +
		"style-src 'self' 'unsafe-inline' https://unpkg.com https://cdnjs.cloudflare.com https://fonts.googleapis.com; " +
		"img-src 'self' data: blob: https://*.basemaps.cartocdn.com https://tile.openstreetmap.org https://*.openstreetmap.org; " +
		"media-src 'self' blob:; " +
		"connect-src 'self' https://unpkg.com https://cdn.jsdelivr.net; " +
		"font-src 'self' https://fonts.gstatic.com; " +
		"worker-src blob:; " +
		"frame-ancestors 'none';"
	c.Header("Content-Security-Policy", csp)

	if !initializers.IsDevelopment {
		c.Header("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
	}

	c.Next()
}
