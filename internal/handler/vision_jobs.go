package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// visionJob 非同步辨識工作（T2.5 辨識進度回報，polling 版）。
// 本機視覺模型一張可能要數十秒；同步 endpoint 會讓前端乾等。
// async=1 時立即回 job_id，前端每 2 秒 poll VisionJobStatus 顯示經過秒數。
type visionJob struct {
	ID      string
	Status  string // running | done | error
	Started time.Time
	Result  map[string]interface{}
	Err     string
}

var (
	visionJobsMu sync.Mutex
	visionJobs   = map[string]*visionJob{}
)

const visionJobTTL = 15 * time.Minute

func newVisionJobID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// startVisionJob 在背景執行 fn 並立即回傳 job ID。
// fn 回傳與同步 endpoint 相同格式的辨識結果。
func startVisionJob(fn func() (map[string]interface{}, error)) string {
	visionJobsMu.Lock()
	now := time.Now()
	// 順手清理過期工作，避免 map 無上限成長。
	for id, j := range visionJobs {
		if now.Sub(j.Started) > visionJobTTL {
			delete(visionJobs, id)
		}
	}
	j := &visionJob{ID: newVisionJobID(), Status: "running", Started: now}
	visionJobs[j.ID] = j
	visionJobsMu.Unlock()

	go func() {
		result, err := fn()
		visionJobsMu.Lock()
		defer visionJobsMu.Unlock()
		if err != nil {
			j.Status, j.Err = "error", err.Error()
		} else {
			j.Status, j.Result = "done", result
		}
	}()
	return j.ID
}

// VisionJobStatus GET /api/vision/jobs/{id}：polling 辨識進度。
// done 時把辨識結果（reading 或 items）併入回傳，前端一次拿齐。
func VisionJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	visionJobsMu.Lock()
	j, ok := visionJobs[id]
	var resp map[string]interface{}
	if ok {
		resp = map[string]interface{}{
			"ok":         true,
			"status":     j.Status,
			"elapsed_ms": time.Since(j.Started).Milliseconds(),
		}
		if j.Status == "done" {
			for k, v := range j.Result {
				resp[k] = v
			}
		}
		if j.Status == "error" {
			resp["error"] = j.Err
		}
	}
	visionJobsMu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "error": "辨識工作不存在或已過期"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}