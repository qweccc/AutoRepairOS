package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func jobStatusRequest(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/vision/jobs/"+id, nil)
	r.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	VisionJobStatus(rec, r)
	return rec
}

func TestVisionJobDone(t *testing.T) {
	id := startVisionJob(func() (map[string]interface{}, error) {
		return map[string]interface{}{"reading": map[string]string{"plate_number": "ABC-1234"}}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec := jobStatusRequest(t, id)
		if rec.Code != http.StatusOK {
			t.Fatalf("status code=%d body=%s", rec.Code, rec.Body.String())
		}
		var j map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
			t.Fatal(err)
		}
		if j["status"] == "done" {
			if j["reading"] == nil {
				t.Fatalf("done 但沒有 reading: %v", j)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job 未在期限內完成: %v", j)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVisionJobError(t *testing.T) {
	id := startVisionJob(func() (map[string]interface{}, error) {
		return nil, errors.New("模型超時")
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec := jobStatusRequest(t, id)
		var j map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
			t.Fatal(err)
		}
		if j["status"] == "error" {
			if j["error"] != "模型超時" {
				t.Fatalf("error 訊息不對: %v", j)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job 未在期限內失敗: %v", j)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVisionJobNotFound(t *testing.T) {
	rec := jobStatusRequest(t, "nonexistent")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知 job 應 404，got %d", rec.Code)
	}
}

func TestVisionJobTTLPurge(t *testing.T) {
	oldID := startVisionJob(func() (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	})
	visionJobsMu.Lock()
	visionJobs[oldID].Started = time.Now().Add(-visionJobTTL - time.Minute)
	visionJobsMu.Unlock()

	startVisionJob(func() (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	})

	visionJobsMu.Lock()
	_, stillThere := visionJobs[oldID]
	visionJobsMu.Unlock()
	if stillThere {
		t.Fatal("過期 job 應被清理")
	}
}