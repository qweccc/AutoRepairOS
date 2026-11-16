package pages

import (
	"context"
	"net/url"
	"time"

	"autorepairos/internal/i18n"
)

func viewLabel(ctx context.Context, view string) string {
	if view == "week" {
		return i18n.T(ctx, "appointment.calendar.week")
	}
	return i18n.T(ctx, "appointment.calendar.day")
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
