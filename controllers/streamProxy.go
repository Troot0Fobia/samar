package controllers

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"sync"

	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"

	"github.com/gin-gonic/gin"
)

func cameraViewerURL() string {
	if v := os.Getenv("CAMERA_VIEWER_URL"); v != "" {
		return v
	}
	return "http://localhost:8787"
}

func newStreamProxy() *httputil.ReverseProxy {
	target, _ := url.Parse(cameraViewerURL())
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		helpers.LogError("camera_viewer proxy error", "", err.Error())
		http.Error(w, `{"error":"stream service unavailable"}`, http.StatusBadGateway)
	}
	return proxy
}

var (
	streamProxyOnce sync.Once
	streamProxyInst *httputil.ReverseProxy
)

func getStreamProxy() *httputil.ReverseProxy {
	streamProxyOnce.Do(func() {
		streamProxyInst = newStreamProxy()
	})
	return streamProxyInst
}

func internalSecret() string {
	if v := os.Getenv("CAMERA_VIEWER_SECRET"); v != "" {
		return v
	}
	if !initializers.IsDevelopment {
		log.Fatal("CAMERA_VIEWER_SECRET must be set in production")
	}
	return "dev-secret"
}

// GetStreamPage renders the camera viewer HTML page.
func GetStreamPage(c *gin.Context) {
	_, role, _ := middleware.CheckAuth(c)
	c.HTML(http.StatusOK, "stream.html", gin.H{
		"isModer": role == "moderator" || role == "admin",
		"isAdmin": role == "admin",
	})
}

// GetStreamChannels discovers available streams for a camera.
// GET /api/stream/channels/:id
func GetStreamChannels(c *gin.Context) {
	cam, ok := loadCamera(c)
	if !ok {
		return
	}
	rewriteAndProxy(c, cam, "/internal/stream/channels")
}

// StreamOpen opens/ensures a stream session for a camera.
// POST /api/stream/open/:id
func StreamOpen(c *gin.Context) {
	cam, ok := loadCamera(c)
	if !ok {
		return
	}
	rewriteAndProxy(c, cam, "/internal/stream/open")
}

// StreamStatus returns the current status of a stream session.
// GET /api/stream/status/:id
func StreamStatus(c *gin.Context) {
	cam, ok := loadCamera(c)
	if !ok {
		return
	}
	key := fmt.Sprintf("%s:%s", cam.IP, cam.Port)
	rewriteAndProxy(c, cam, "/internal/stream/status/"+url.PathEscape(key))
}

// WSStream proxies a WebSocket connection to camera_viewer.
// GET /ws/stream/:id
func WSStream(c *gin.Context) {
	cam, ok := loadCamera(c)
	if !ok {
		return
	}
	key := fmt.Sprintf("%s:%s", cam.IP, cam.Port)
	// Camera credentials forwarded as internal headers to camera_viewer (localhost:8787).
	// Safe because the connection is loopback-only and gated by X-Internal-Secret.
	c.Request.Header.Set("X-Camera-IP", cam.IP)
	c.Request.Header.Set("X-Camera-Port", cam.Port)
	c.Request.Header.Set("X-Camera-Login", cam.Login)
	c.Request.Header.Set("X-Camera-Password", cam.Password)
	c.Request.Header.Set("X-Internal-Secret", internalSecret())
	c.Request.URL.Path = "/ws/stream/" + url.PathEscape(key)
	getStreamProxy().ServeHTTP(c.Writer, c.Request)
}

// RecordStart starts a recording session for a camera.
// POST /api/record/start/:id
func RecordStart(c *gin.Context) {
	cam, ok := loadCamera(c)
	if !ok {
		return
	}
	userID := currentUserID(c)
	c.Request.Header.Set("X-Camera-IP", cam.IP)
	c.Request.Header.Set("X-Camera-Port", cam.Port)
	c.Request.Header.Set("X-Camera-Login", cam.Login)
	c.Request.Header.Set("X-Camera-Password", cam.Password)
	c.Request.Header.Set("X-Camera-Key", fmt.Sprintf("%s:%s", cam.IP, cam.Port))
	c.Request.Header.Set("X-User-ID", strconv.FormatUint(uint64(userID), 10))
	c.Request.Header.Set("X-Internal-Secret", internalSecret())
	c.Request.URL.Path = "/internal/record/start"
	getStreamProxy().ServeHTTP(c.Writer, c.Request)
}

// RecordStop stops an active recording.
// POST /api/record/stop
func RecordStop(c *gin.Context) { proxyInternal(c, "/internal/record/stop") }

// RecordList lists recordings (filtered by user unless admin).
// GET /api/record/list
func RecordList(c *gin.Context) {
	_, role, _ := middleware.CheckAuth(c)
	path := "/internal/record/list"
	if role != "admin" {
		q := url.Values{}
		q.Set("user_id", strconv.FormatUint(uint64(currentUserID(c)), 10))
		path += "?" + q.Encode()
	}
	proxyInternal(c, path)
}

// RecordDownload serves a recording file for download.
// GET /api/record/download/:rec_id
func RecordDownload(c *gin.Context) {
	proxyInternal(c, "/internal/record/download/"+url.PathEscape(c.Param("rec_id")))
}

// RecordDelete deletes a recording (moderator+).
// DELETE /api/record/:rec_id
func RecordDelete(c *gin.Context) {
	proxyInternal(c, "/internal/record/"+url.PathEscape(c.Param("rec_id")))
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func loadCamera(c *gin.Context) (models.Camera, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid camera id"})
		return models.Camera{}, false
	}
	var cam models.Camera
	if err := initializers.DB.First(&cam, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return cam, false
	}
	return cam, true
}

func rewriteAndProxy(c *gin.Context, cam models.Camera, internalPath string) {
	c.Request.Header.Set("X-Camera-IP", cam.IP)
	c.Request.Header.Set("X-Camera-Port", cam.Port)
	c.Request.Header.Set("X-Camera-Login", cam.Login)
	c.Request.Header.Set("X-Camera-Password", cam.Password)
	c.Request.Header.Set("X-Camera-Key", fmt.Sprintf("%s:%s", cam.IP, cam.Port))
	c.Request.Header.Set("X-Internal-Secret", internalSecret())
	c.Request.URL.Path = internalPath
	getStreamProxy().ServeHTTP(c.Writer, c.Request)
}

func proxyInternal(c *gin.Context, internalPath string) {
	c.Request.Header.Set("X-Internal-Secret", internalSecret())
	c.Request.URL.Path = internalPath
	getStreamProxy().ServeHTTP(c.Writer, c.Request)
}

func currentUserID(c *gin.Context) uint {
	if val, exists := c.Get("userID"); exists {
		if id, ok := val.(uint); ok {
			return id
		}
	}
	return 0
}
