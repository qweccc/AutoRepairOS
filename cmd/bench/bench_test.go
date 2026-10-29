package main

import (
	"testing"
	"time"
)

// TestBenchmarkThresholds 驗證門檻判斷邏輯
func TestBenchmarkThresholds(t *testing.T) {
	tests := []struct {
		name      string
		duration  time.Duration
		threshold time.Duration
		want      bool
	}{
		{"通過", 200 * time.Millisecond, 300 * time.Millisecond, true},
		{"剛好通過", 299 * time.Millisecond, 300 * time.Millisecond, true},
		{"失敗", 301 * time.Millisecond, 300 * time.Millisecond, false},
		{"失敗很多", 500 * time.Millisecond, 300 * time.Millisecond, false},
		{"通知通過", 59 * time.Second, 1 * time.Minute, true},
		{"通知失敗", 61 * time.Second, 1 * time.Minute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.duration < tt.threshold
			if got != tt.want {
				t.Errorf("門檻判斷錯誤: duration=%v, threshold=%v, got=%v, want=%v",
					tt.duration, tt.threshold, got, tt.want)
			}
		})
	}
}

// TestScaleDefault 驗證 --scale 參數預設值
func TestScaleDefault(t *testing.T) {
	defaultScale := 10000
	if *scale != defaultScale {
		t.Errorf("預設 scale 應為 %d, 得到 %d", defaultScale, *scale)
	}
}
