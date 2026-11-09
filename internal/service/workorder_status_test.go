package service

import "testing"

func TestValidateWorkOrderTransition(t *testing.T) {
	cases := []struct {
		name    string
		current string
		target  string
		wantErr bool
	}{
		{"pending->in_progress", "pending", "in_progress", false},
		{"pending->waiting_parts", "pending", "waiting_parts", false},
		{"in_progress->completed", "in_progress", "completed", false},
		{"completed->in_progress", "completed", "in_progress", false},
		{"paid->anything", "paid", "completed", true},
		{"invalid transition", "pending", "completed", true},
		{"same status", "pending", "pending", false},
		{"target paid", "pending", "paid", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateWorkOrderTransition(c.current, c.target)
			if (err != nil) != c.wantErr {
				t.Fatalf("got err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}