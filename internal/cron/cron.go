package cron

import (
	"time"
)

// init 註冊所有內建排程任務。
//
// 排程器核心（Scheduler、tick、runTask、DB 時間輔助）位於 cron_engine.go；
// 通知佇列（QueueNotification、processNotifications 等）位於 cron_notifications.go；
// 每日備份位於 cron_backup.go；提醒掃描（保養/檢驗/保單/生日/預約/完工補掃）
// 位於 cron_reminders.go。
func init() {
	Register(Task{
		Key:      "process_notifications",
		Name:     "通知佇列處理",
		Interval: 1 * time.Minute,
		Run:      processNotifications,
	})

	Register(Task{
		Key:      "daily_backup",
		Name:     "每日自動備份",
		Interval: 24 * time.Hour,
		Run:      DailyBackup,
	})

	Register(Task{
		Key:      "work_order_ready",
		Name:     "工作單就緒掃描",
		Interval: 6 * time.Hour,
		Run:      scanWorkOrderReady,
	})

	Register(Task{
		Key:      "maintenance_due_scan",
		Name:     "維護到期掃描",
		Interval: 24 * time.Hour,
		Run:      scanMaintenanceDue,
	})

	Register(Task{
		Key:      "appointment_reminder_scan",
		Name:     "預約提醒掃描",
		Interval: 24 * time.Hour,
		Run:      scanAppointmentReminders,
	})

	Register(Task{
		Key:      "birthday_reminder_scan",
		Name:     "生日提醒掃描",
		Interval: 24 * time.Hour,
		Run:      scanBirthdayReminders,
	})
}