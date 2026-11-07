package handler

import (
	"testing"

)

func TestAllowedTransitions(t *testing.T) {
	tests := []struct {
		from     string
		to       string
		expected bool
	}{
		{"pending", "in_progress", true},
		{"pending", "waiting_parts", true},
		{"pending", "completed", false},
		{"pending", "paid", false},
		{"in_progress", "waiting_parts", true},
		{"in_progress", "completed", true},
		{"in_progress", "pending", false},
		{"waiting_parts", "in_progress", true},
		{"waiting_parts", "completed", true},
		{"completed", "in_progress", true},
		{"completed", "paid", false},
		{"paid", "pending", false},
		{"paid", "in_progress", false},
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			allowed := false
			for _, s := range allowedTransitions[tt.from] {
				if s == tt.to {
					allowed = true
					break
				}
			}
			if allowed != tt.expected {
				t.Errorf("transition %s->%s: got %v, want %v", tt.from, tt.to, allowed, tt.expected)
			}
		})
	}
}

func TestWorkOrderStatusLabel(t *testing.T) {
	expected := []string{"pending", "in_progress", "waiting_parts", "completed", "paid"}
	for _, s := range expected {
		if _, ok := workOrderStatusLabel[s]; !ok {
			t.Errorf("status %s not in workOrderStatusLabel", s)
		}
	}
}

func TestShouldCheckTransition(t *testing.T) {
	if !shouldCheckTransition("pending", "completed") {
		t.Error("should check")
	}
	if shouldCheckTransition("", "completed") {
		t.Error("empty current should not check")
	}
	if shouldCheckTransition("pending", "pending") {
		t.Error("same status should not check")
	}
	if shouldCheckTransition("pending", "paid") {
		t.Error("target paid should not check")
	}
}
