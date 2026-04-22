package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // max requests
	window   time.Duration // per window
	stop     chan struct{}
}

type visitor struct {
	count     int
	windowEnd time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
		stop:     make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) Stop() {
	close(rl.stop)
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, v := range rl.visitors {
				if now.After(v.windowEnd) {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}

func (rl *rateLimiter) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// c.ClientIP() reads X-Forwarded-For; requires router.SetTrustedProxies() to be
		// configured with the upstream Nginx address, otherwise the header can be spoofed.
		ip := c.ClientIP()
		rl.mu.Lock()
		v, exists := rl.visitors[ip]
		now := time.Now()
		if !exists || now.After(v.windowEnd) {
			rl.visitors[ip] = &visitor{count: 1, windowEnd: now.Add(rl.window)}
			rl.mu.Unlock()
			c.Next()
			return
		}
		// v.count is incremented before the check so exactly `rate` requests are allowed per window.
		v.count++
		if v.count > rl.rate {
			rl.mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		rl.mu.Unlock()
		c.Next()
	}
}

// LoginLimiter: 10 requests per minute per IP.
var LoginLimiter = newRateLimiter(10, time.Minute)

// RegisterLimiter: 5 requests per 10 minutes per IP.
var RegisterLimiter = newRateLimiter(5, 10*time.Minute)

// GeoSearchLimiter: 10 requests per minute per IP for Nominatim proxy.
// Nominatim ToS requires no more than 1 req/s — this enforces a per-user
// budget that keeps aggregate load well within that limit.
var GeoSearchLimiter = newRateLimiter(10, time.Minute)
