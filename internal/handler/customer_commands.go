package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"autorepairos/internal/db"
)

// 客戶聊天指令共用層（LINE webhook 與 Telegram getUpdates 共用）。
// 綁定欄位依通道切換：line→line_user_id、telegram→telegram_user_id。

func channelColumn(channel string) string {
	switch channel {
	case "telegram":
		return "telegram_user_id"
	case "discord":
		return "discord_user_id"
	case "slack":
		return "slack_user_id"
	default:
		return "line_user_id"
	}
}

func resolveCustomerByChannel(channel, userID string) (int64, bool) {
	var id int64
	err := db.DB.QueryRow(`SELECT id FROM active_customers WHERE `+channelColumn(channel)+` = ?`, userID).Scan(&id)
	return id, err == nil
}

const unboundReply = "查不到您的客戶綁定資料，請先完成綁定（向櫃檯索取綁定碼，輸入「綁定 綁定碼」）。"

// handleCustomerCommand 兩通道共用的指令分派：預約對話優先，其次進度/工單/履歷。
func handleCustomerCommand(channel, userID, q string) string {
	q = strings.TrimSpace(q)

	// 進行中的預約對話優先攔截（FR-APP-06）；過期或無對話則走一般指令。
	if reply, handled := handleBookingMessage(channel, userID, q); handled {
		return reply
	}

	switch {
	case q == "預約" || q == "我要預約" || q == "掛號":
		return startBooking(channel, userID)

	case q == "履歷" || q == "維修紀錄" || q == "保養紀錄":
		cid, ok := resolveCustomerByChannel(channel, userID)
		if !ok {
			return unboundReply
		}
		return customerPassbookSummaryByID(cid)

	case q == "進度" || q == "查詢" || q == "工單":
		cid, ok := resolveCustomerByChannel(channel, userID)
		if !ok {
			return "查不到您的車輛紀錄，請確認手機號碼是否已登錄。"
		}
		return workOrderProgressSummary(cid)

	case strings.HasPrefix(q, "工單"):
		idStr := strings.TrimSpace(strings.TrimPrefix(q, "工單"))
		woID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return "請輸入工單編號，例如「工單 12」。"
		}
		cid, ok := resolveCustomerByChannel(channel, userID)
		if !ok {
			return unboundReply
		}
		return workOrderStatusByID(cid, woID)

	default:
		return "您好，請輸入「預約」掛號預約、「進度」查詢車輛狀態、「工單 編號」查指定工單，或「履歷」查詢維修紀錄。"
	}
}

// workOrderProgressSummary 最新工單狀態。
func workOrderProgressSummary(customerID int64) string {
	var plate, status string
	err := db.DB.QueryRow(`SELECT v.plate_number, wo.status
		FROM active_work_orders wo JOIN active_vehicles v ON v.id = wo.vehicle_id
		WHERE wo.customer_id = ? ORDER BY wo.id DESC LIMIT 1`, customerID).Scan(&plate, &status)
	if err != nil {
		return "查不到您的車輛紀錄，請確認手機號碼是否已登錄。"
	}
	label := workOrderStatusLabel(context.Background(), status)
	if label == "" {
		label = status
	}
	return fmt.Sprintf("您的車輛 %s 目前狀態：%s", plate, label)
}

// workOrderStatusByID 指定工單狀態（限本人）。
func workOrderStatusByID(customerID, woID int64) string {
	var plate, status string
	err := db.DB.QueryRow(`SELECT v.plate_number, wo.status
		FROM active_work_orders wo JOIN active_vehicles v ON v.id = wo.vehicle_id
		WHERE wo.id = ? AND wo.customer_id = ?`, woID, customerID).Scan(&plate, &status)
	if err != nil {
		return "找不到這張工單，或它不是您的車輛。"
	}
	label := workOrderStatusLabel(context.Background(), status)
	if label == "" {
		label = status
	}
	return fmt.Sprintf("工單 #%d（%s）狀態：%s", woID, plate, label)
}