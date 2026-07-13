package controllers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"Troot0Fobia/samar/cinema"
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"
	"Troot0Fobia/samar/snapshot"

	"github.com/gin-gonic/gin"
)

// ─── Ad-hoc cinema cameras ────────────────────────────────────────────────────
//
// Cameras added from the Cinema page live only in this in-memory registry:
// they are never written to the DB and disappear when the entry expires or
// the server restarts. Their IDs start at adhocIDBase so they can never
// collide with real Camera rows.

const (
	adhocIDBase     uint = 1 << 30
	adhocIdleTTL         = 2 * time.Hour // refreshed on every lookup (SSE dispatch, WS connect)
	adhocMaxEntries      = 256           // server-wide abuse guard
)

type adhocEntry struct {
	cam      models.Camera // synthetic; cam.ID = assigned ad-hoc ID
	protocol string        // "dahua" | "rtsp" | "unknown"
	lastUsed time.Time
}

type adhocRegistry struct {
	mu     sync.Mutex
	nextID uint
	m      map[uint]*adhocEntry
}

var adhocCams = &adhocRegistry{nextID: adhocIDBase, m: map[uint]*adhocEntry{}}
var adhocCleanupOnce sync.Once

func (r *adhocRegistry) add(cam models.Camera, protocol string) (uint, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.m) >= adhocMaxEntries {
		return 0, false
	}
	id := r.nextID
	r.nextID++
	cam.ID = id
	r.m[id] = &adhocEntry{cam: cam, protocol: protocol, lastUsed: time.Now()}
	adhocCleanupOnce.Do(func() { go r.cleanupLoop() })
	return id, true
}

func (r *adhocRegistry) get(id uint) (models.Camera, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.m[id]
	if !ok {
		return models.Camera{}, "", false
	}
	e.lastUsed = time.Now()
	return e.cam, e.protocol, true
}

// setLink stores the RTSP link discovered by the unknown-protocol fallback and
// flips the protocol to "rtsp" so later SSE/WS lookups go straight to RTSP.
func (r *adhocRegistry) setLink(id uint, link string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.m[id]; ok {
		e.cam.Link = link
		e.protocol = "rtsp"
		e.lastUsed = time.Now()
	}
}

func (r *adhocRegistry) setChannels(id uint, chJSON string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.m[id]; ok {
		e.cam.Channels = chJSON
		e.lastUsed = time.Now()
	}
}

func (r *adhocRegistry) remove(id uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}

func (r *adhocRegistry) cleanupLoop() {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for range tick.C {
		r.mu.Lock()
		for id, e := range r.m {
			if time.Since(e.lastUsed) > adhocIdleTTL {
				delete(r.m, id)
			}
		}
		r.mu.Unlock()
	}
}

// loadCinemaCamera resolves a cinema camera ID: ad-hoc registry first, then DB.
func loadCinemaCamera(id uint) (models.Camera, string, bool) {
	if id >= adhocIDBase {
		return adhocCams.get(id)
	}
	var cam models.Camera
	if err := initializers.DB.Preload("MaintainerRef").First(&cam, id).Error; err != nil {
		return models.Camera{}, "", false
	}
	return cam, resolveDBCameraProtocol(cam), true
}

// resolveDBCameraProtocol infers the cinema protocol for a real (non-ad-hoc)
// Camera row: an explicit RTSP Link always wins, otherwise a camera tagged
// with the "Hikvision" maintainer uses the native ISAPI/SDK client, and
// everything else falls back to Dahua DVRIP (the historical default).
func resolveDBCameraProtocol(cam models.Camera) string {
	if cam.Link != "" {
		return "rtsp"
	}
	if cam.MaintainerRef != nil && strings.EqualFold(cam.MaintainerRef.Name, "hikvision") {
		return "hikvision"
	}
	return "dahua"
}

// cacheCinemaChannels routes the discovered-channels cache: ad-hoc cameras
// keep it in the registry, real cameras in the DB column as before.
func cacheCinemaChannels(cam *models.Camera, chJSON string) {
	if cam.ID >= adhocIDBase {
		adhocCams.setChannels(cam.ID, chJSON)
		return
	}
	initializers.DB.Model(cam).Update("channels", chJSON)
}

// probeUnknownCinema mirrors the snapshot engine's order for unknown vendors:
// Dahua DVRIP first, then a generated rtsp://[login:pass@]ip:port|554/ URL.
func probeUnknownCinema(ctx context.Context, cam models.Camera, events chan<- string) {
	if probeDahuaCinema(ctx, cam, events) {
		return
	}
	if ctx.Err() != nil {
		return
	}
	encoded, _ := snapshot.BuildGeneratedRTSP(cam)
	if !cinema.ProbeRTSP(encoded) {
		return // both protocols failed; the dahua "offline" event was already sent
	}
	adhocCams.setLink(cam.ID, encoded)
	cam.Link = encoded
	probeRTSPCinema(ctx, cam, events)
}

// ─── HTTP handlers ────────────────────────────────────────────────────────────

// AddCinemaAdhocCamera serves POST /api/cinema/adhoc_camera
func AddCinemaAdhocCamera(c *gin.Context) {
	var body struct {
		Protocol string `json:"protocol"`
		IP       string `json:"ip"`
		Port     string `json:"port"`
		Login    string `json:"login"`
		Password string `json:"password"`
		Link     string `json:"link"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "невалидный запрос"})
		return
	}

	var cam models.Camera
	switch body.Protocol {
	case "rtsp":
		link := strings.TrimSpace(body.Link)
		if link == "" || len(link) > 1024 || !strings.HasPrefix(strings.ToLower(link), "rtsp://") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "невалидная RTSP-ссылка"})
			return
		}
		u, err := url.Parse(link)
		if err != nil || u.Host == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "невалидная RTSP-ссылка"})
			return
		}
		cam = models.Camera{Name: u.Host, Link: link}
	case "dahua", "hikvision", "unknown":
		if !helpers.ValidateIP(body.IP) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "невалидный IP-адрес"})
			return
		}
		if body.Port != "" && !helpers.ValidatePort(body.Port) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "невалидный порт"})
			return
		}
		if len(body.Login) > 128 || len(body.Password) > 128 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "слишком длинные логин или пароль"})
			return
		}
		cam = models.Camera{Name: body.IP, IP: body.IP, Port: body.Port, Login: body.Login, Password: body.Password}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "невалидный протокол"})
		return
	}

	id, ok := adhocCams.add(cam, body.Protocol)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "слишком много временных камер, попробуйте позже"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// DeleteCinemaAdhocCamera serves DELETE /api/cinema/adhoc_camera/:id
func DeleteCinemaAdhocCamera(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || uint(id) < adhocIDBase {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	adhocCams.remove(uint(id))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
