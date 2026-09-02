package controllers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"
	"Troot0Fobia/samar/snapshot"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SnapshotStart starts (or resumes) a snapshot run. Admin only.
func SnapshotStart(c *gin.Context) {
	var body struct {
		Resume     bool   `json:"resume"`
		Workers    int    `json:"workers"`
		Filter     string `json:"filter"`
		AllCameras bool   `json:"allCameras"` // legacy compat
	}
	c.ShouldBindJSON(&body)

	// Support legacy allCameras bool
	if body.AllCameras && body.Filter == "" {
		body.Filter = "all"
	}
	if body.Filter == "" {
		body.Filter = "known"
	}
	if body.Workers < 1 {
		body.Workers = 1
	}
	if body.Workers > 20 {
		body.Workers = 20
	}

	_, _, username := middleware.CheckAuth(c)
	var user models.User
	var userID uint
	if err := initializers.DB.Where("username = ?", username).First(&user).Error; err == nil {
		userID = user.ID
	}

	if err := snapshot.Global.Start(body.Resume, body.Workers, body.Filter, userID); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// SnapshotStop stops a running snapshot run. Admin only.
func SnapshotStop(c *gin.Context) {
	snapshot.Global.Stop()
	c.JSON(200, gin.H{"ok": true})
}

// SnapshotStatus returns the latest run status. Moder+.
func SnapshotStatus(c *gin.Context) {
	var run models.SnapshotRun
	if err := initializers.DB.Order("created_at DESC").First(&run).Error; err != nil {
		c.JSON(200, gin.H{"status": "idle"})
		return
	}
	c.JSON(200, gin.H{
		"runId":     run.ID,
		"status":    run.Status,
		"total":     run.TotalCameras,
		"processed": run.ProcessedCount,
		"success":   run.SuccessCount,
		"errors":    run.ErrorCount,
		"workers":   run.Workers,
		"filter":    run.Filter,
	})
}

// SnapshotRuns returns all snapshot runs ordered by date. Moder+.
func SnapshotRuns(c *gin.Context) {
	var runs []models.SnapshotRun
	initializers.DB.Order("created_at DESC").Find(&runs)

	type runRow struct {
		ID             uint       `json:"id"`
		Status         string     `json:"status"`
		Filter         string     `json:"filter"`
		Workers        int        `json:"workers"`
		TotalCameras   int        `json:"totalCameras"`
		ProcessedCount int        `json:"processedCount"`
		SuccessCount   int        `json:"successCount"`
		ErrorCount     int        `json:"errorCount"`
		CreatedAt      time.Time  `json:"createdAt"`
		FinishedAt     *time.Time `json:"finishedAt"`
	}

	rows := make([]runRow, 0, len(runs))
	for _, r := range runs {
		rows = append(rows, runRow{
			ID:             r.ID,
			Status:         r.Status,
			Filter:         r.Filter,
			Workers:        r.Workers,
			TotalCameras:   r.TotalCameras,
			ProcessedCount: r.ProcessedCount,
			SuccessCount:   r.SuccessCount,
			ErrorCount:     r.ErrorCount,
			CreatedAt:      r.CreatedAt,
			FinishedAt:     r.FinishedAt,
		})
	}
	c.JSON(200, rows)
}

// SnapshotReport returns all results of the specified run (or latest). Moder+.
func SnapshotReport(c *gin.Context) {
	var run models.SnapshotRun
	if runIDStr := c.Query("runId"); runIDStr != "" {
		runID, err := strconv.ParseUint(runIDStr, 10, 64)
		if err != nil || initializers.DB.First(&run, runID).Error != nil {
			c.JSON(404, gin.H{"error": "run not found"})
			return
		}
	} else {
		if err := initializers.DB.Order("created_at DESC").First(&run).Error; err != nil {
			c.JSON(200, gin.H{"results": []any{}})
			return
		}
	}

	var results []models.SnapshotResult
	initializers.DB.Where("run_id = ?", run.ID).
		Preload("Camera", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }). // include soft-deleted cameras so report rows keep name/ip
		Order("id ASC").
		Find(&results)

	type row struct {
		ID             uint        `json:"id"`
		CameraID       uint        `json:"cameraId"`
		IP             string      `json:"ip"`
		Port           string      `json:"port"`
		Name           string      `json:"name"`
		CamStatus      string      `json:"camStatus"`
		Login          string      `json:"login"`
		Pass           string      `json:"pass"`
		Link           string      `json:"link"`
		Lat            float64     `json:"lat"`
		Lng            float64     `json:"lng"`
		UsedMethod     string      `json:"usedMethod"`
		WasUnknown     bool        `json:"wasUnknown"`
		GeneratedLink  string      `json:"generatedLink"`
		MaintainerName string      `json:"maintainerName"`
		ErrorType      string      `json:"errorType"`
		ErrorMsg       string      `json:"errorMsg"`
		ChannelsFound  int         `json:"channelsFound"`
		ChannelsDone   int         `json:"channelsDone"`
		ChannelErrors  interface{} `json:"channelErrors"`
		Snaps          []string    `json:"snaps"`
		PrevSnaps      []string    `json:"prevSnaps"`
	}

	unmarshalStrings := func(s string) []string {
		if s == "" {
			return nil
		}
		var out []string
		json.Unmarshal([]byte(s), &out)
		return out
	}

	rows := make([]row, 0, len(results))
	for _, r := range results {
		cam := r.Camera

		// Use per-run stored file list; fall back to filesystem scan for old rows.
		snaps := unmarshalStrings(r.SnapsJSON)
		prevSnaps := unmarshalStrings(r.PrevSnapsJSON)
		if snaps == nil && r.ChannelsDone > 0 {
			snaps, prevSnaps = getSnapFiles(cam.IP, cam.Port)
		}

		var chErrs interface{}
		if r.ChannelErrors != "" {
			json.Unmarshal([]byte(r.ChannelErrors), &chErrs)
		}

		rows = append(rows, row{
			ID:             r.ID,
			CameraID:       r.CameraID,
			IP:             cam.IP,
			Port:           cam.Port,
			Name:           cam.Name,
			CamStatus:      cam.Status,
			Login:          cam.Login,
			Pass:           cam.Password,
			Link:           cam.Link,
			Lat:            cam.Lat,
			Lng:            cam.Lng,
			UsedMethod:     r.UsedMethod,
			WasUnknown:     r.WasUnknown,
			GeneratedLink:  r.GeneratedLink,
			MaintainerName: r.MaintainerName,
			ErrorType:      r.ErrorType,
			ErrorMsg:       r.ErrorMsg,
			ChannelsFound:  r.ChannelsFound,
			ChannelsDone:   r.ChannelsDone,
			ChannelErrors:  chErrs,
			Snaps:          snaps,
			PrevSnaps:      prevSnaps,
		})
	}

	c.JSON(200, gin.H{
		"runId":     run.ID,
		"status":    run.Status,
		"filter":    run.Filter,
		"workers":   run.Workers,
		"total":     run.TotalCameras,
		"processed": run.ProcessedCount,
		"success":   run.SuccessCount,
		"errors":    run.ErrorCount,
		"results":   rows,
	})
}

// SnapshotDownload sends a CSV report. Admin only.
func SnapshotDownload(c *gin.Context) {
	var run models.SnapshotRun
	if runIDStr := c.Query("runId"); runIDStr != "" {
		runID, err := strconv.ParseUint(runIDStr, 10, 64)
		if err != nil || initializers.DB.First(&run, runID).Error != nil {
			c.String(404, "Run not found")
			return
		}
	} else {
		if err := initializers.DB.Order("created_at DESC").First(&run).Error; err != nil {
			c.String(404, "No snapshot runs found")
			return
		}
	}

	var results []models.SnapshotResult
	initializers.DB.Where("run_id = ?", run.ID).
		Preload("Camera", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }). // include soft-deleted cameras so report rows keep name/ip
		Order("id ASC").
		Find(&results)

	c.Header("Content-Disposition", `attachment; filename="snapshot_report.csv"`)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Writer.Write([]byte("\xEF\xBB\xBF")) // BOM for Excel

	w := csv.NewWriter(c.Writer)
	w.Write([]string{"Название", "IP:Port", "Login:Pass", "RTSP Link", "Координаты", "Метод", "Тип ошибки", "Сообщение ошибки", "Каналов найдено", "Каналов успешно"})
	for _, r := range results {
		cam := r.Camera
		coords := fmt.Sprintf("%v,%v", cam.Lat, cam.Lng)
		w.Write([]string{
			cam.Name,
			cam.IP + ":" + cam.Port,
			cam.Login + ":" + cam.Password,
			cam.Link,
			coords,
			r.UsedMethod,
			r.ErrorType,
			r.ErrorMsg,
			strconv.Itoa(r.ChannelsFound),
			strconv.Itoa(r.ChannelsDone),
		})
	}
	w.Flush()
}

// SnapshotApplyMaintainers sets the Dahua/Hikvision maintainer on cameras that
// were resolved via that protocol in the given run and still have no
// maintainer assigned. RTSP-resolved cameras are never touched. Admin only.
func SnapshotApplyMaintainers(c *gin.Context) {
	var run models.SnapshotRun
	if runIDStr := c.Query("runId"); runIDStr != "" {
		runID, err := strconv.ParseUint(runIDStr, 10, 64)
		if err != nil || initializers.DB.First(&run, runID).Error != nil {
			c.JSON(404, gin.H{"error": "run not found"})
			return
		}
	} else {
		if err := initializers.DB.Order("created_at DESC").First(&run).Error; err != nil {
			c.JSON(404, gin.H{"error": "no snapshot runs found"})
			return
		}
	}

	var maintainers []models.Maintainer
	initializers.DB.Where("name IN ?", []string{"Dahua", "Hikvision"}).Find(&maintainers)
	maintainerIDByMethod := map[string]uint{}
	for _, m := range maintainers {
		maintainerIDByMethod[strings.ToLower(m.Name)] = m.ID
	}
	if maintainerIDByMethod["dahua"] == 0 || maintainerIDByMethod["hikvision"] == 0 {
		c.JSON(500, gin.H{"error": "dahua/hikvision maintainer records missing"})
		return
	}

	var results []models.SnapshotResult
	initializers.DB.Where("run_id = ? AND used_method IN ?", run.ID, []string{"dahua", "hikvision"}).Find(&results)

	updated := 0
	for _, r := range results {
		maintainerID, ok := maintainerIDByMethod[r.UsedMethod]
		if !ok {
			continue
		}
		res := initializers.DB.Model(&models.Camera{}).
			Where("id = ? AND maintainer_id IS NULL", r.CameraID).
			Update("maintainer_id", maintainerID)
		if res.RowsAffected > 0 {
			updated++
		}
	}

	c.JSON(200, gin.H{"ok": true, "updated": updated})
}

// SnapshotWS handles the WebSocket connection for real-time snapshot events. Moder+.
func SnapshotWS(c *gin.Context) {
	conn, err := wsUpgradeCinema(c.Writer, c.Request)
	if err != nil {
		return
	}
	defer conn.Close()

	sub, unsub := snapshot.Global.Subscribe()
	defer unsub()

	wsCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wsReadLoop(conn, cancel)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case evt, ok := <-sub:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			if err := wsSendTextFrame(conn, data); err != nil {
				return
			}
		case <-ticker.C:
			data, _ := json.Marshal(snapshot.Event{Type: "ping"})
			if err := wsSendTextFrame(conn, data); err != nil {
				return
			}
		case <-wsCtx.Done():
			return
		}
	}
}

func getSnapFiles(ip, port string) (snaps, prevSnaps []string) {
	dir := filepath.Join("data", "photos", ip)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := fmt.Sprintf("%s_%s_snap_ch", ip, port)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(strings.ToLower(name), ".jpg") {
			continue
		}
		encoded := url.QueryEscape(name)
		if strings.HasSuffix(name, "_prev.jpg") {
			prevSnaps = append(prevSnaps, encoded)
		} else {
			snaps = append(snaps, encoded)
		}
	}
	return
}
