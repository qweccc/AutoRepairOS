package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/cron"
	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// ListScheduledTasks 顯示排程任務狀態、通知佇列與備份紀錄。
func ListScheduledTasks(w http.ResponseWriter, r *http.Request, user model.User) {
	statuses := cron.Status()
	var taskRows []pages.ScheduledTaskRow
	for _, s := range statuses {
		taskRows = append(taskRows, pages.ScheduledTaskRow{Name: s.Name, Key: s.Key, Interval: s.IntervalLabel,
			LastRun: orDash(s.LastRunAt), NextRun: orDash(s.NextRunAt), RunCount: s.RunCount,
			LastError: s.LastError, Enabled: s.Enabled})
	}

	// 通知紀錄：同時顯示待處理（可取消）與近期歷史（含失敗狀態）
	pending := cron.PendingNotifications(50)
	recent := cron.RecentNotifications(50)
	var notificationRows []pages.NotificationRow

	// LINE 連線狀態：讓管理者一眼看出通知是否真的會外送。
	lineCfg, _ := line.Shared()
	linePill := "p-pending"
	if lineCfg.Ready() {
		linePill = "p-completed"
	} else if !lineCfg.Mock {
		linePill = "p-apprno"
	}

	kindLabels := map[string]string{
		"maintenance_due":   "保養到期",
		"work_order_ready":  "完工通知",
		"estimate_approval": "估價核准",
	}
	channelLabels := map[string]string{"line": "LINE", "telegram": "Telegram", "internal": "內部", "none": "不通知"}

	type statusStyle struct{ Label, Pill string }
	statusPills := map[string]statusStyle{
		"pending":           {"待處理", "p-waitparts"},
		"processing":        {"處理中", "p-waitparts"},
		"sent":              {"已送達", "p-completed"},
		"cancelled":         {"已取消", "p-pending"},
		"failed":            {"失敗", "p-apprno"},
		"permanent_failure": {"永久失敗", "p-apprno"},
	}

	for _, n := range recent {
		kindLabel := kindLabels[n.Kind]
		if kindLabel == "" {
			kindLabel = n.Kind
		}
		chLabel := channelLabels[n.Channel]
		if chLabel == "" {
			chLabel = n.Channel
		}
		st, ok := statusPills[n.Status]
		label, pill := n.Status, "p-pending"
		if ok {
			label, pill = st.Label, st.Pill
		}

		detail := ""
		if n.RetryCount > 0 {
			detail = "失敗次數 " + strconv.Itoa(n.RetryCount)
		}
		if n.ErrorMessage != "" {
			if detail != "" {
				detail += "；"
			}
			detail += n.ErrorMessage
		}
		notificationRows = append(notificationRows, pages.NotificationRow{ID: n.ID, Kind: kindLabel, Channel: chLabel,
			Subject: orDash(n.Subject), Body: truncateStr(n.Body, 70), ScheduledAt: n.ScheduledAt,
			Status: n.Status, StatusLabel: label, StatusPill: pill, ErrorDetail: detail, Cancelable: n.Status == "pending"})

	}

	// 備份紀錄
	rows, err := db.DB.Query(`SELECT backup_date, COALESCE(db_backup_path,''),
		COALESCE(db_size,0), COALESCE(photo_count,0), COALESCE(photo_size,0),
		status, COALESCE(error_message,'')
		FROM backup_records ORDER BY id DESC LIMIT 30`)
	var backupRowsData []pages.BackupRow
	if err == nil {
		for rows.Next() {
			var date, path, status, errMsg string
			var dbSize, photoCount, photoSize int64
			if err := rows.Scan(&date, &path, &dbSize, &photoCount, &photoSize, &status, &errMsg); err != nil {
				continue
			}
			backupRowsData = append(backupRowsData, pages.BackupRow{Date: date, Path: orDash(path), DBSize: sizeKB(dbSize),
				PhotoCount: strconv.FormatInt(photoCount, 10), PhotoSize: sizeKB(photoSize), Success: status == "success", Error: errMsg})
		}
		rows.Close()
	}

	pages.ListScheduledTasksPage(taskRows, notificationRows, backupRowsData,
		strconv.Itoa(len(pending)), strconv.Itoa(len(recent)),
		linePill, lineCfg.StatusLine(),
		user).Render(r.Context(), w)
}

func retryStatusDetail(n cron.Notification) string {
	var details []string
	if n.RetryCount > 0 {
		details = append(details, "失敗次數 "+strconv.Itoa(n.RetryCount))
	}
	if n.ErrorMessage != "" {
		details = append(details, htmlEsc(n.ErrorMessage))
	}
	if len(details) == 0 {
		return ""
	}
	return `<br><small style="color:#fca5a5">` + strings.Join(details, "；") + `</small>`
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
