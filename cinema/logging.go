package cinema

import (
	"io"
	"os"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// cameraLogDest is the shared destination for per-camera protocol logs
// (Dahua DVRIP + Hikvision ISAPI wire-level exchanges) — kept in their own
// rotated file under logs/ since they're high-volume raw request/response
// traces, separate from Logrus's app-level access/error logs (see
// initializers/initLoggers.go). Still mirrored to stderr so `go run .`
// piped through `tee`/redirected output keeps showing them live.
var (
	cameraLogOnce sync.Once
	cameraLogDest io.Writer
)

func cameraLogWriter() io.Writer {
	cameraLogOnce.Do(func() {
		rotator := &lumberjack.Logger{
			Filename:   "./logs/camera_requests.log",
			MaxSize:    50, // MB per file before rotating
			MaxBackups: 5,  // rotated files to keep
			MaxAge:     14, // days before a rotated file is deleted
			Compress:   true,
		}
		cameraLogDest = io.MultiWriter(os.Stderr, rotator)
	})
	return cameraLogDest
}
