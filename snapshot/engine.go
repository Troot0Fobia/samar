package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Troot0Fobia/samar/cinema"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"

	"gorm.io/gorm"
)

type channelError struct {
	Ch      int    `json:"ch"`
	ErrType string `json:"errType"`
	ErrMsg  string `json:"errMsg"`
}

type Event struct {
	Type          string         `json:"type"`
	RunID         uint           `json:"runId,omitempty"`
	Processed     int            `json:"processed,omitempty"`
	Total         int            `json:"total,omitempty"`
	CameraID      uint           `json:"cameraId,omitempty"`
	IP            string         `json:"ip,omitempty"`
	Port          string         `json:"port,omitempty"`
	Name          string         `json:"name,omitempty"`
	Login         string         `json:"login,omitempty"`
	Pass          string         `json:"pass,omitempty"`
	Link          string         `json:"link,omitempty"`
	Lat           float64        `json:"lat,omitempty"`
	Lng           float64        `json:"lng,omitempty"`
	UsedMethod     string         `json:"usedMethod,omitempty"`
	WasUnknown     bool           `json:"wasUnknown,omitempty"`
	GeneratedLink  string         `json:"generatedLink,omitempty"`
	MaintainerName string         `json:"maintainerName,omitempty"`
	ErrorType      string         `json:"errorType,omitempty"`
	ErrorMsg      string         `json:"errorMsg,omitempty"`
	ChannelsFound int            `json:"channelsFound,omitempty"`
	ChannelsDone  int            `json:"channelsDone,omitempty"`
	ChannelErrors []channelError `json:"channelErrors,omitempty"`
	Snaps         []string       `json:"snaps,omitempty"`
	PrevSnaps     []string       `json:"prevSnaps,omitempty"`
	Status        string         `json:"status,omitempty"`
	Success       int            `json:"success,omitempty"`
	Errors        int            `json:"errors,omitempty"`
}

type Engine struct {
	mu      sync.Mutex
	runID   uint
	running bool
	cancel  context.CancelFunc
	subs    map[chan Event]struct{}
}

var Global = &Engine{
	subs: make(map[chan Event]struct{}),
}

func (e *Engine) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs, ch)
		close(ch)
		e.mu.Unlock()
	}
}

func (e *Engine) broadcast(evt Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- evt:
		default:
		}
	}
}

func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *Engine) Start(resume bool, workers int, filter string, userID uint) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return fmt.Errorf("snapshot already running")
	}

	if workers < 1 {
		workers = 1
	}
	if workers > 20 {
		workers = 20
	}
	if filter != "known" && filter != "unknown" && filter != "all" {
		filter = "known"
	}

	db := initializers.DB

	var run models.SnapshotRun
	if resume {
		if err := db.Where("status IN ?", []string{"running", "stopped"}).
			Order("created_at DESC").
			First(&run).Error; err != nil {
			return fmt.Errorf("no stopped run to resume: %w", err)
		}
		run.Status = "running"
		run.FinishedAt = nil
		db.Save(&run)
	} else {
		// Count cameras matching filter for accurate total
		q := applyFilter(db.Model(&models.Camera{}), filter)
		var total int64
		q.Count(&total)
		run = models.SnapshotRun{
			Status:       "running",
			TotalCameras: int(total),
			Workers:      workers,
			Filter:       filter,
			StartedByID:  userID,
		}
		db.Create(&run)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.running = true
	e.runID = run.ID
	go e.processCameras(ctx, run)
	return nil
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running || e.cancel == nil {
		return
	}
	e.cancel()
}

func applyFilter(q *gorm.DB, filter string) *gorm.DB {
	if filter == "all" || filter == "" {
		return q
	}
	q = q.Joins("LEFT JOIN maintainers ON maintainers.id = cameras.maintainer_id AND maintainers.deleted_at IS NULL")
	switch filter {
	case "known":
		return q.Where("cameras.link != '' OR maintainers.name = 'Dahua'")
	case "unknown":
		return q.Where("cameras.link = '' AND (cameras.maintainer_id IS NULL OR maintainers.name != 'Dahua')")
	}
	return q
}

func (e *Engine) processCameras(ctx context.Context, run models.SnapshotRun) {
	defer func() {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
	}()

	db := initializers.DB

	// Proper resume: exclude already-processed cameras
	var doneIDs []uint
	db.Model(&models.SnapshotResult{}).Where("run_id = ?", run.ID).Pluck("camera_id", &doneIDs)

	q := db.Order("cameras.id ASC")
	if len(doneIDs) > 0 {
		q = q.Where("cameras.id NOT IN ?", doneIDs)
	}
	q = applyFilter(q, run.Filter)
	q = q.Preload("MaintainerRef")

	var cameras []models.Camera
	q.Find(&cameras)

	// Recalculate total to account for already-done on resume
	total := len(cameras) + len(doneIDs)
	if run.TotalCameras != total {
		run.TotalCameras = total
		db.Save(&run)
	}

	workers := run.Workers
	if workers < 1 {
		workers = 1
	}

	workCh := make(chan models.Camera, workers*2)
	var wg sync.WaitGroup

	var mu sync.Mutex
	processed := run.ProcessedCount
	success := run.SuccessCount
	errCount := run.ErrorCount

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cam := range workCh {
				result := e.processCamera(ctx, cam, run.ID)

				// Increment processed only after completion so parallel workers
				// never broadcast a lower count than a previously finished one.
				mu.Lock()
				processed++
				curProcessed := processed
				curTotal := run.TotalCameras
				if result.ErrorType != "" {
					errCount++
				} else {
					success++
				}
				run.ProcessedCount = processed
				run.SuccessCount = success
				run.ErrorCount = errCount
				db.Save(&run)
				mu.Unlock()

				e.broadcast(Event{
					Type:           "progress",
					RunID:          run.ID,
					Processed:      curProcessed,
					Total:          curTotal,
					CameraID:       cam.ID,
					IP:             cam.IP,
					Port:           cam.Port,
					Name:           cam.Name,
					Login:          cam.Login,
					Pass:           cam.Password,
					Link:           cam.Link,
					Lat:            cam.Lat,
					Lng:            cam.Lng,
					UsedMethod:     result.UsedMethod,
					WasUnknown:     result.WasUnknown,
					GeneratedLink:  result.GeneratedLink,
					MaintainerName: result.MaintainerName,
					ErrorType:      result.ErrorType,
					ErrorMsg:       result.ErrorMsg,
					ChannelsFound:  result.ChannelsFound,
					ChannelsDone:   result.ChannelsDone,
					ChannelErrors:  result.ChannelErrors,
					Snaps:          result.Snaps,
					PrevSnaps:      result.PrevSnaps,
				})
			}
		}()
	}

	stopped := false
	for _, cam := range cameras {
		select {
		case <-ctx.Done():
			stopped = true
		case workCh <- cam:
			continue
		}
		break
	}
	close(workCh)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if stopped || ctx.Err() != nil {
		run.Status = "stopped"
		run.ProcessedCount = processed
		run.SuccessCount = success
		run.ErrorCount = errCount
		db.Save(&run)
		e.broadcast(Event{
			Type:    "statusChange",
			RunID:   run.ID,
			Status:  "stopped",
			Total:   run.TotalCameras,
			Success: success,
			Errors:  errCount,
		})
		return
	}

	now := time.Now()
	run.Status = "finished"
	run.FinishedAt = &now
	run.ProcessedCount = processed
	run.SuccessCount = success
	run.ErrorCount = errCount
	db.Save(&run)
	e.broadcast(Event{
		Type:    "statusChange",
		RunID:   run.ID,
		Status:  "finished",
		Total:   run.TotalCameras,
		Success: success,
		Errors:  errCount,
	})
}

type camResult struct {
	UsedMethod     string
	WasUnknown     bool
	GeneratedLink  string
	MaintainerName string
	ErrorType      string
	ErrorMsg       string
	ChannelsFound  int
	ChannelsDone   int
	ChannelErrors  []channelError
	Snaps          []string
	PrevSnaps      []string
}

func (e *Engine) processCamera(ctx context.Context, cam models.Camera, runID uint) camResult {
	db := initializers.DB

	wasUnknown := cam.Link == "" && (cam.MaintainerRef == nil || cam.MaintainerRef.Name != "Dahua")

	maintainerName := ""
	if cam.MaintainerRef != nil {
		maintainerName = cam.MaintainerRef.Name
	}

	camCtx, camCancel := context.WithTimeout(ctx, 120*time.Second)
	defer camCancel()

	var result camResult
	var generatedLink string

	if cam.Link != "" {
		result = e.snapshotRTSP(camCtx, cam)
		result.UsedMethod = "rtsp"
	} else if wasUnknown {
		encodedURL, displayURL := buildGeneratedRTSP(cam)
		chCtx, chCancel := context.WithTimeout(camCtx, 20*time.Second)
		snap, prev, errType, _ := snapshotRTSPChannel(chCtx, cam, encodedURL, 0)
		chCancel()
		if errType == "" {
			result.UsedMethod = "rtsp"
			result.ChannelsFound = 1
			result.ChannelsDone = 1
			result.Snaps = []string{snap}
			if prev != "" {
				result.PrevSnaps = []string{prev}
			}
			generatedLink = displayURL
		} else {
			result = e.snapshotDahua(camCtx, cam)
			result.UsedMethod = "dahua"
		}
	} else {
		result = e.snapshotDahua(camCtx, cam)
		result.UsedMethod = "dahua"
	}

	result.WasUnknown = wasUnknown
	result.GeneratedLink = generatedLink
	result.MaintainerName = maintainerName

	marshal := func(v any) string {
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}

	chErrJSON := ""
	if len(result.ChannelErrors) > 0 {
		chErrJSON = marshal(result.ChannelErrors)
	}

	db.Create(&models.SnapshotResult{
		RunID:          runID,
		CameraID:       cam.ID,
		UsedMethod:     result.UsedMethod,
		WasUnknown:     wasUnknown,
		GeneratedLink:  result.GeneratedLink,
		MaintainerName: result.MaintainerName,
		ErrorType:      result.ErrorType,
		ErrorMsg:       result.ErrorMsg,
		ChannelsFound:  result.ChannelsFound,
		ChannelsDone:   result.ChannelsDone,
		ChannelErrors:  chErrJSON,
		SnapsJSON:      marshal(result.Snaps),
		PrevSnapsJSON:  marshal(result.PrevSnaps),
		ProcessedAt:    time.Now(),
	})

	return result
}

// buildGeneratedRTSP returns (encodedURL, displayURL).
// encodedURL uses url.URL to percent-encode userinfo correctly for ffmpeg.
// displayURL keeps raw credentials for human-readable display.
func buildGeneratedRTSP(cam models.Camera) (encoded, display string) {
	port := cam.Port
	if port == "" {
		port = "554"
	}
	login, pass := cam.Login, cam.Password
	bare := fmt.Sprintf("rtsp://%s:%s/", cam.IP, port)
	if login == "-" || pass == "-" || (login == "" && pass == "") {
		return bare, bare
	}
	u := &url.URL{
		Scheme: "rtsp",
		User:   url.UserPassword(login, pass),
		Host:   cam.IP + ":" + port,
		Path:   "/",
	}
	display = fmt.Sprintf("rtsp://%s:%s@%s:%s/", login, pass, cam.IP, port)
	return u.String(), display
}

func (e *Engine) snapshotDahua(ctx context.Context, cam models.Camera) camResult {
	addr := fmt.Sprintf("%s:%s", cam.IP, cam.Port)

	type connectRes struct {
		client *cinema.Client
		err    error
	}
	connCh := make(chan connectRes, 1)
	go func() {
		c, err := cinema.NewClient(addr, cam.Login, cam.Password, "snap:"+cam.IP)
		connCh <- connectRes{c, err}
	}()

	var client *cinema.Client
	select {
	case <-ctx.Done():
		go func() {
			if r := <-connCh; r.client != nil {
				r.client.Close()
			}
		}()
		return camResult{ErrorType: "timeout", ErrorMsg: "connection timeout"}
	case r := <-connCh:
		if r.err != nil {
			return classifyConnError(r.err)
		}
		client = r.client
	}
	defer client.Close()

	channels := client.ListChannels()

	seen := map[int]bool{}
	var mainChs []cinema.ChannelInfo
	for _, ch := range channels {
		if ch.SubType == 0 && !seen[ch.Index] {
			seen[ch.Index] = true
			mainChs = append(mainChs, ch)
		}
	}

	result := camResult{ChannelsFound: len(mainChs)}
	for i, ch := range mainChs {
		if ctx.Err() != nil {
			// Camera budget exhausted — mark all remaining channels so the UI count is accurate.
			for _, rem := range mainChs[i:] {
				result.ChannelErrors = append(result.ChannelErrors, channelError{
					Ch:      rem.Index,
					ErrType: "timeout",
					ErrMsg:  "camera timeout reached",
				})
			}
			break
		}

		chCtx, chCancel := context.WithTimeout(ctx, 30*time.Second)
		snapFile, prevFile, chErr := snapshotDahuaChannel(chCtx, client, cam, ch.Index)
		chCancel()

		if chErr == nil {
			result.ChannelsDone++
			result.Snaps = append(result.Snaps, snapFile)
			if prevFile != "" {
				result.PrevSnaps = append(result.PrevSnaps, prevFile)
			}
		} else {
			errType, errMsg := classifyChannelError(chErr)
			result.ChannelErrors = append(result.ChannelErrors, channelError{
				Ch:      ch.Index,
				ErrType: errType,
				ErrMsg:  errMsg,
			})
		}
	}

	if result.ChannelsDone == 0 && result.ChannelsFound > 0 {
		result.ErrorType = "camera_error"
		result.ErrorMsg = "no channels could be snapshotted"
	}
	return result
}

func snapshotDahuaChannel(ctx context.Context, client *cinema.Client, cam models.Camera, chIdx int) (snapFile, prevFile string, err error) {
	type streamRes struct {
		s   *cinema.Stream
		err error
	}
	sCh := make(chan streamRes, 1)
	go func() {
		s, e := client.OpenStream(chIdx, 0)
		sCh <- streamRes{s, e}
	}()

	var stream *cinema.Stream
	select {
	case <-ctx.Done():
		go func() {
			if r := <-sCh; r.s != nil {
				r.s.Close()
			}
		}()
		return "", "", ctx.Err()
	case r := <-sCh:
		if r.err != nil {
			return "", "", r.err
		}
		stream = r.s
	}
	defer stream.Close()

	type peekRes struct {
		codec string
		err   error
	}
	pCh := make(chan peekRes, 1)
	go func() {
		codec, e := stream.PeekFirstFrame()
		pCh <- peekRes{codec, e}
	}()

	var codec string
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case r := <-pCh:
		if r.err != nil {
			return "", "", r.err
		}
		codec = r.codec
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-f", codec,
		"-i", "pipe:0",
		"-vframes", "1",
		"-q:v", "2",
		"-f", "image2",
		"pipe:1",
	)
	cmd.Stdin = stream
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	jpegBytes, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", "", fmt.Errorf("timeout")
		}
		return "", "", fmt.Errorf("ffmpeg: %s", truncateErr(stderr.String()))
	}
	if len(jpegBytes) == 0 {
		return "", "", fmt.Errorf("ffmpeg produced no output")
	}

	return archiveAndSave(cam.IP, cam.Port, chIdx, jpegBytes)
}

func (e *Engine) snapshotRTSP(ctx context.Context, cam models.Camera) camResult {
	rawURL := injectCreds(cam.Link, cam.Login, cam.Password)
	mode := cinema.DetectRTSPMode(rawURL)

	var channels []cinema.RTSPChannel
	switch mode {
	case cinema.RTSPModeTemplate:
		chs, ok := cinema.ExpandTemplate(rawURL)
		if !ok {
			return camResult{ErrorType: "camera_error", ErrorMsg: "template expansion failed"}
		}
		for i := range chs {
			chs[i].URL = injectCreds(chs[i].URL, cam.Login, cam.Password)
		}
		channels = chs
	case cinema.RTSPModeAuto:
		enumCtx, enumCancel := context.WithTimeout(ctx, 90*time.Second)
		chs := cinema.EnumerateRTSPChannels(enumCtx, rawURL)
		enumCancel()
		if len(chs) == 0 {
			return camResult{ErrorType: "camera_error", ErrorMsg: "no RTSP channels found"}
		}
		channels = chs
	default:
		channels = []cinema.RTSPChannel{{URL: rawURL, Label: "Main"}}
	}

	result := camResult{ChannelsFound: len(channels)}

	for i, ch := range channels {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		chCtx, chCancel := context.WithTimeout(ctx, 20*time.Second)
		snapFile, prevFile, errType, errMsg := snapshotRTSPChannel(chCtx, cam, ch.URL, i)
		chCancel()

		if errType == "" {
			result.ChannelsDone++
			result.Snaps = append(result.Snaps, snapFile)
			if prevFile != "" {
				result.PrevSnaps = append(result.PrevSnaps, prevFile)
			}
		} else {
			result.ChannelErrors = append(result.ChannelErrors, channelError{
				Ch:      i,
				ErrType: errType,
				ErrMsg:  errMsg,
			})
		}
	}

	if result.ChannelsDone == 0 && len(result.ChannelErrors) > 0 {
		last := result.ChannelErrors[len(result.ChannelErrors)-1]
		result.ErrorType = last.ErrType
		result.ErrorMsg = last.ErrMsg
	}
	return result
}

func snapshotRTSPChannel(ctx context.Context, cam models.Camera, rtspURL string, chIdx int) (snapFile, prevFile, errType, errMsg string) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-vframes", "1",
		"-q:v", "2",
		"-f", "image2",
		"pipe:1",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	jpegBytes, err := cmd.Output()
	if err != nil {
		msg := stderr.String()
		if ctx.Err() != nil {
			return "", "", "timeout", "timeout"
		}
		if strings.Contains(msg, "401") || strings.Contains(msg, "Unauthorized") {
			return "", "", "wrong_creds", truncateErr(msg)
		}
		if strings.Contains(msg, "refused") || strings.Contains(msg, "No route") ||
			strings.Contains(msg, "timed out") || strings.Contains(msg, "unreachable") {
			return "", "", "network_error", truncateErr(msg)
		}
		return "", "", "camera_error", truncateErr(msg)
	}
	if len(jpegBytes) == 0 {
		return "", "", "camera_error", "ffmpeg produced no output"
	}

	s, p, saveErr := archiveAndSave(cam.IP, cam.Port, chIdx, jpegBytes)
	if saveErr != nil {
		return "", "", "camera_error", saveErr.Error()
	}
	return s, p, "", ""
}

func archiveAndSave(ip, port string, chIdx int, data []byte) (current, prev string, err error) {
	dir := filepath.Join("data", "photos", ip)
	if err = os.MkdirAll(dir, 0755); err != nil {
		return
	}
	base := fmt.Sprintf("%s_%s_snap_ch%d", ip, port, chIdx)
	currentPath := filepath.Join(dir, base+".jpg")
	prevPath := filepath.Join(dir, base+"_prev.jpg")

	os.Remove(prevPath)
	if _, statErr := os.Stat(currentPath); statErr == nil {
		os.Rename(currentPath, prevPath)
		prev = base + "_prev.jpg"
	}

	if err = os.WriteFile(currentPath, data, 0644); err != nil {
		return
	}
	current = base + ".jpg"
	return
}

func injectCreds(rawURL, login, pass string) string {
	if login == "" || login == "-" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil {
		return rawURL
	}
	u.User = url.UserPassword(login, pass)
	return u.String()
}

func classifyConnError(err error) camResult {
	msg := err.Error()
	var errType string
	switch {
	case strings.Contains(msg, "refused") || strings.Contains(msg, "no route") ||
		strings.Contains(msg, "unreachable") || strings.Contains(msg, "network"):
		errType = "network_error"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out"):
		errType = "timeout"
	default:
		errType = "camera_error"
	}
	return camResult{ErrorType: errType, ErrorMsg: truncateErr(msg)}
}

func classifyChannelError(err error) (errType, errMsg string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "timeout") || err.Error() == "context deadline exceeded":
		return "timeout", truncateErr(msg)
	case strings.Contains(msg, "refused") || strings.Contains(msg, "no route") ||
		strings.Contains(msg, "unreachable"):
		return "network_error", truncateErr(msg)
	default:
		return "camera_error", truncateErr(msg)
	}
}

func truncateErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
