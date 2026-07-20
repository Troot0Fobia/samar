package cinema

import (
	"io"
	"log"
	"os"
	"sync"
)

// cameraLogDest is the shared destination for per-camera protocol logs
// (Dahua DVRIP + Hikvision ISAPI wire-level exchanges) — kept in their own
// file under logs/ since they're high-volume raw request/response traces,
// separate from Logrus's app-level access/error logs (see
// initializers/initLoggers.go). Still mirrored to stderr so `go run .`
// piped through `tee`/redirected output keeps showing them live.
var (
	cameraLogOnce sync.Once
	cameraLogDest io.Writer = os.Stderr
)

func cameraLogWriter() io.Writer {
	cameraLogOnce.Do(func() {
		f, err := os.OpenFile("./logs/camera_requests.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Printf("cinema: failed to open logs/camera_requests.log, logging to stderr only: %v", err)
			return
		}
		cameraLogDest = io.MultiWriter(os.Stderr, f)
	})
	return cameraLogDest
}
