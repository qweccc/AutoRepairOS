package pages

import (
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/a-h/templ"
)

func AppointmentCalendar(date, view, status string, days []CalendarDay) templ.Component {
	var out strings.Builder
	out.WriteString(`<div id="appointment-calendar"><form hx-get="/api/appointments/calendar" hx-trigger="change" hx-target="#appointment-calendar" hx-swap="outerHTML" style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:12px">`)
	out.WriteString(`<label>視圖<select name="view"><option value="day"`)
	if view == "day" {
		out.WriteString(` selected`)
	}
	out.WriteString(`>日</option><option value="week"`)
	if view == "week" {
		out.WriteString(` selected`)
	}
	out.WriteString(`>週</option></select></label><label>日期<input type="date" name="date" value="`)
	out.WriteString(html.EscapeString(date))
	out.WriteString(`"></label><label>狀態<select name="status"><option value=""`)
	if status == "" {
		out.WriteString(` selected`)
	}
	out.WriteString(`>全部狀態</option>`)
	for _, option := range []struct{ value, label string }{
		{"booked", "已預約"}, {"confirmed", "已確認"}, {"arrived", "已進廠"},
		{"completed", "已完成"}, {"cancelled", "已取消"},
	} {
		out.WriteString(`<option value="` + option.value + `"`)
		if status == option.value {
			out.WriteString(` selected`)
		}
		out.WriteString(`>` + option.label + `</option>`)
	}
	out.WriteString(`</select></label><a class="btn btn-s" href="`)
	out.WriteString(html.EscapeString(calendarURL(date, view, status, -1)))
	out.WriteString(`" hx-get="`)
	out.WriteString(html.EscapeString(calendarURL(date, view, status, -1)))
	out.WriteString(`" hx-target="#appointment-calendar" hx-swap="outerHTML">上一` + viewLabel(view) + `</a><a class="btn btn-s" href="`)
	out.WriteString(html.EscapeString(calendarURL(date, view, status, 1)))
	out.WriteString(`" hx-get="`)
	out.WriteString(html.EscapeString(calendarURL(date, view, status, 1)))
	out.WriteString(`" hx-target="#appointment-calendar" hx-swap="outerHTML">下一` + viewLabel(view) + `</a></form>`)

	for _, day := range days {
		out.WriteString(`<h3 style="margin:12px 0 6px">` + html.EscapeString(day.Label) + `</h3>`)
		if len(day.Appointments) == 0 {
			out.WriteString(`<div class="empty">此日沒有預約</div>`)
			continue
		}
		out.WriteString(`<table><tr><th>時段</th><th>車牌／客戶</th><th>工位</th><th>服務內容</th><th>狀態</th></tr>`)
		for _, appointment := range day.Appointments {
			out.WriteString(`<tr><td>` + html.EscapeString(appointment.AppointmentTime) + `</td><td>` +
				html.EscapeString(appointment.PlateNumber) + `／` + html.EscapeString(appointment.CustomerName) +
				`</td><td>` + html.EscapeString(appointment.BayLabel) + `</td><td>` +
				html.EscapeString(appointment.ServiceType) + `</td><td><span class="pill ` +
				html.EscapeString(appointment.StatusPill) + `">` + html.EscapeString(appointment.StatusLabel) +
				`</span></td></tr>`)
		}
		out.WriteString(`</table>`)
	}
	out.WriteString(`</div>`)
	return templ.Raw(out.String())
}

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
