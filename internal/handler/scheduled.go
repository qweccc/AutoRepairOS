package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/cron"
	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ListScheduledTasks 顯示排程任務狀態、通知佇列與備份紀錄。
func ListScheduledTasks(w http.ResponseWriter, r *http.Request, user model.User) {
	statuses := cron.Status()

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>任務</th><th>識別碼</th><th>頻率</th><th>狀態</th><th>上次執行</th><th>下次執行</th><th>次數</th><th>操作</th></tr>`)
	for _, s := range statuses {
		pill := `<span class="pill p-completed">啟用</span>`
		toggleBtn := `<a class="btn btn-o" href="/scheduled-tasks/` + htmlEsc(s.Key) + `/toggle">停用</a>`
		if !s.Enabled {
			pill = `<span class="pill p-pending">停用</span>`
			toggleBtn = `<a class="btn btn-p" href="/scheduled-tasks/` + htmlEsc(s.Key) + `/toggle">啟用</a>`
		}
		lastErr := ""
		if s.LastError != "" {
			lastErr = `<br><span style="color:#fca5a5;font-size:11.5px">錯誤：` + htmlEsc(s.LastError) + `</span>`
		}
		tb.WriteString(`<tr><td><b>` + htmlEsc(s.Name) + `</b>` + lastErr + `</td><td><code>` + htmlEsc(s.Key) +
			`</code></td><td>` + htmlEsc(s.IntervalLabel) + `</td><td>` + pill +
			`</td><td>` + htmlEsc(orDash(s.LastRunAt)) + `</td><td>` + htmlEsc(orDash(s.NextRunAt)) +
			`</td><td class="num">` + strconv.Itoa(s.RunCount) + `</td><td>` + toggleBtn + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	tasksTable := tb.String()
	if len(statuses) == 0 {
		tasksTable = `<div class="empty">尚未註冊任何排程任務</div>`
	}

	// 通知佇列
	pending := cron.PendingNotifications(50)
	var nb strings.Builder
	nb.WriteString(`<table><tr><th>#</th><th>類型</th><th>通道</th><th>主旨</th><th>內容</th><th>排定時間</th><th>狀態</th><th>操作</th></tr>`)
	for _, n := range pending {
		kindLabel := map[string]string{
			"maintenance_due":   "保養到期",
			"work_order_ready":  "完工通知",
			"estimate_approval": "估價核准",
		}[n.Kind]
		if kindLabel == "" {
			kindLabel = n.Kind
		}
		chLabel := map[string]string{"line": "LINE", "sms": "SMS", "internal": "內部", "none": "不通知"}[n.Channel]
		if chLabel == "" {
			chLabel = n.Channel
		}
		nb.WriteString(`<tr><td>` + strconv.FormatInt(n.ID, 10) + `</td><td>` + htmlEsc(kindLabel) +
			`</td><td>` + htmlEsc(chLabel) + `</td><td>` + htmlEsc(orDash(n.Subject)) +
			`</td><td style="max-width:340px">` + htmlEsc(truncateStr(n.Body, 70)) +
			`</td><td>` + htmlEsc(n.ScheduledAt) + `</td><td><span class="pill p-waitparts">待處理</span>` +
			`</td><td><a class="btn btn-d" href="/notifications/` + strconv.FormatInt(n.ID, 10) +
			`/cancel" onclick="return confirm('取消這筆通知？')">取消</a></td></tr>`)
	}
	nb.WriteString(`</table>`)

	notifTable := nb.String()
	if len(pending) == 0 {
		notifTable = `<div class="empty">佇列為空</div>`
	}

	// 備份紀錄
	rows, err := db.DB.Query(`SELECT backup_date, COALESCE(db_backup_path,''),
		COALESCE(db_size,0), COALESCE(photo_count,0), COALESCE(photo_size,0),
		status, COALESCE(error_message,'')
		FROM backup_records ORDER BY id DESC LIMIT 30`)
	var bk strings.Builder
	bk.WriteString(`<table><tr><th>日期</th><th>備份檔</th><th class="num">DB 大小</th><th class="num">照片數</th><th class="num">照片大小</th><th>狀態</th></tr>`)
	backupRows := 0
	if err == nil {
		for rows.Next() {
			var date, path, status, errMsg string
			var dbSize, photoCount, photoSize int64
			if err := rows.Scan(&date, &path, &dbSize, &photoCount, &photoSize, &status, &errMsg); err != nil {
				continue
			}
			backupRows++
			pill := `<span class="pill p-completed">成功</span>`
			if status != "success" {
				pill = `<span class="pill p-apprno">失敗</span>`
			}
			bk.WriteString(`<tr><td>` + htmlEsc(date) + `</td><td style="font-size:11.5px">` +
				htmlEsc(orDash(path)) + `</td><td class="num">` + sizeKB(dbSize) +
				`</td><td class="num">` + strconv.FormatInt(photoCount, 10) +
				`</td><td class="num">` + sizeKB(photoSize) + `</td><td>` + pill +
				errLine(errMsg) + `</td></tr>`)
		}
		rows.Close()
	}
	bk.WriteString(`</table>`)

	backupTable := bk.String()
	if backupRows == 0 {
		backupTable = `<div class="empty">尚無備份紀錄</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>排程任務</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        排程器每分鐘檢查一次到期任務。單一任務失敗不會中斷其他任務。
      </p>` + tasksTable + `
      <div style="margin-top:14px">
        <a class="btn btn-p" href="/scheduled-tasks/backups/run">立即執行備份</a>
        <a class="btn btn-s" href="/scheduled-tasks/notifications/run">立即處理通知佇列</a>
        <a class="btn btn-s" href="/scheduled-tasks/maintenance-due/run">立即掃描保養到期</a>
      </div>
    </div>

    <div class="card">
      <h2>通知佇列（待處理 ` + strconv.Itoa(len(pending)) + ` 筆）</h2>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 14px">
        LINE/SMS 通道已預留，實際外送待 Phase 4 整合；目前標記為已送達以避免佇列堆積。
      </p>` + notifTable + `
    </div>

    <div class="card">
      <h2>備份紀錄</h2>` + backupTable + `
    </div>`

	renderPage(w, "排程與備份", "/scheduled-tasks", user.Username, user.Role, body)
}

// ToggleScheduledTask 啟用或停用排程任務。
func ToggleScheduledTask(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	res, err := db.DB.Exec("UPDATE scheduled_tasks SET enabled = 1 - COALESCE(enabled,0) WHERE task_key = ?", key)
	if err != nil {
		fail(w, "切換任務失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/scheduled-tasks", "任務狀態已切換")
}

// CancelNotification 取消一筆待處理通知。
func CancelNotification(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	res, err := db.DB.Exec("UPDATE notifications SET status='cancelled' WHERE id=? AND status='pending'", id)
	if err != nil {
		fail(w, "取消失動")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/scheduled-tasks", "通知已取消")
}

// RunBackupNow 手動觸發每日備份。
func RunBackupNow(w http.ResponseWriter, r *http.Request) {
	if err := cron.DailyBackup(); err != nil {
		fail(w, "備份執行失敗: "+err.Error())
		return
	}
	redirectMsg(w, r, "/scheduled-tasks", "備份已完成")
}

// RunNotificationsNow 手動觸發通知佇列處理。
func RunNotificationsNow(w http.ResponseWriter, r *http.Request) {
	n := cron.ProcessNotificationsNow()
	redirectMsg(w, r, "/scheduled-tasks", "已處理 "+strconv.Itoa(n)+" 筆通知")
}

// RunMaintenanceScanNow 手動觸發保養到期掃描。
func RunMaintenanceScanNow(w http.ResponseWriter, r *http.Request) {
	n, err := cron.ScanMaintenanceDueNow()
	if err != nil {
		fail(w, "掃描失敗: "+err.Error())
		return
	}
	redirectMsg(w, r, "/scheduled-tasks", "掃描完成，新增 "+strconv.Itoa(n)+" 筆保養提醒")
}

// RunWorkOrderReadyNow 手動觸發完工通知掃描。
func RunWorkOrderReadyNow(w http.ResponseWriter, r *http.Request) {
	n, err := cron.ScanWorkOrderReadyNow()
	if err != nil {
		fail(w, "掃描失敗: "+err.Error())
		return
	}
	redirectMsg(w, r, "/scheduled-tasks", "掃描完成，新增 "+strconv.Itoa(n)+" 筆完工通知")
}

func errLine(msg string) string {
	if strings.TrimSpace(msg) == "" {
		return ""
	}
	return `<br><span style="color:#fca5a5;font-size:11.5px">` + htmlEsc(msg) + `</span>`
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

var _ = time.Now
