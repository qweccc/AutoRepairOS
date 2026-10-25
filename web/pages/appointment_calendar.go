package pages

import (
	"net/url"
	"time"
)

func viewLabel(view string) string {
	if view == "week" {
		return "週"
	}
	return "日"
}

func calendarURL(date, view, status string, offset int) string {
	current, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "/api/appointments/calendar"
	}
	delta := offset
	if view == "week" {
		delta *= 7
	}
	current = current.AddDate(0, 0, delta)
	query := url.Values{}
	query.Set("date", current.Format("2006-01-02"))
	query.Set("view", view)
	query.Set("status", status)
	return "/api/appointments/calendar?" + query.Encode()
}
