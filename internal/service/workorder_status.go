package service

import "fmt"

var allowedWorkOrderTransitions = map[string][]string{
	"pending":       {"in_progress", "waiting_parts"},
	"in_progress":   {"waiting_parts", "completed"},
	"waiting_parts": {"in_progress", "completed"},
	"completed":     {"in_progress"},
	"paid":          {},
}

func ValidateWorkOrderTransition(current, target string) error {
	if current == "" || current == target || target == "paid" {
		return nil
	}
	for _, s := range allowedWorkOrderTransitions[current] {
		if s == target {
			return nil
		}
	}
	return fmt.Errorf("不允許從 %q 轉換為 %q", current, target)
}