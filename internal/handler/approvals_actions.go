package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// ApproveRequestHandler 同意授權端點
func ApproveRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/approval/approve/")
	if token == "" {
		http.Error(w, "無效的授權連結", http.StatusBadRequest)
		return
	}

	req, err := GetApprovalRequestByToken(token)
	if err != nil {
		renderApprovalResult(w, "error", err.Error())
		return
	}

	// 驗證狀態
	if req.Status == "expired" {
		renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		return
	}
	if req.Status == "approved" || req.Status == "rejected" {
		renderApprovalResult(w, "decided", fmt.Sprintf("此授權已%s", map[string]string{"approved": "同意", "rejected": "拒絕"}[req.Status]))
		return
	}

	// 開始交易
	tx, err := db.DB.Begin()
	if err != nil {
		renderApprovalResult(w, "error", "交易失敗")
		return
	}
	defer tx.Rollback()

	// 1. 更新授權狀態
	now := time.Now().Format("2006-01-02 15:04:05")
	result, err := tx.Exec(
		`UPDATE approval_requests
			SET status='approved', decided_at=?, decision_source='web'
			WHERE id = ? AND status = 'pending' AND expires_at > ?`,
		now, req.ID, now,
	)
	if err != nil {
		renderApprovalResult(w, "error", "更新授權狀態失敗")
		return
	}

	// 檢查是否真的更新了（可能狀態已經不是 pending 或已過期）
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		// 重新查詢狀態
		var currentStatus string
		_ = db.DB.QueryRow("SELECT status FROM approval_requests WHERE id = ?", req.ID).Scan(&currentStatus)
		if currentStatus == "expired" {
			renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		} else {
			renderApprovalResult(w, "decided", "此授權已決定")
		}
		return
	}

	// 2. 把授權內容寫進工單項目並重算總額。
	//
	//    必須與「把授權標成 approved」在同一個交易內：分開做的話，中途失敗會
	//    留下「已核准但工單沒有項目」或反向的不一致狀態，而這是錢的問題
	//    （客戶以為同意了、工單卻沒加，或反之）。
	//
	//    與「客戶電話同意」共用 applyApprovalItems：兩條路徑各寫一份的話，
	//    遲早有一邊忘了重算總額——項目進得了明細卻不算進 total_amount＝漏收。
	if err := applyApprovalItems(tx, req); err != nil {
		log.Printf("授權 %d 套用追加項目失敗，已中止: %v", req.ID, err)
		renderApprovalResult(w, "error", "追加項目資料異常，已通知車廠處理")
		return
	}

	_, err = tx.Exec(
		"UPDATE work_orders SET approval_status='approved', updated_at=? WHERE id = ?",
		now, req.WorkOrderID,
	)
	if err != nil {
		renderApprovalResult(w, "error", "更新工單狀態失敗")
		return
	}

	// 提交交易
	if err := tx.Commit(); err != nil {
		renderApprovalResult(w, "error", "提交交易失敗")
		return
	}

	log.Printf("授權已核准: work_order_id=%d", req.WorkOrderID)
	renderApprovalResult(w, "success", "您的同意已送出，車廠會盡快安排維修")
}

// RejectRequestHandler 拒絕授權端點
func RejectRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, "/approval/reject/")
	if token == "" {
		http.Error(w, "無效的授權連結", http.StatusBadRequest)
		return
	}

	req, err := GetApprovalRequestByToken(token)
	if err != nil {
		renderApprovalResult(w, "error", err.Error())
		return
	}

	// 驗證狀態
	if req.Status == "expired" {
		renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		return
	}
	if req.Status == "approved" || req.Status == "rejected" {
		renderApprovalResult(w, "decided", fmt.Sprintf("此授權已%s", map[string]string{"approved": "同意", "rejected": "拒絕"}[req.Status]))
		return
	}

	// 更新授權狀態
	now := time.Now().Format("2006-01-02 15:04:05")
	result, err := db.DB.Exec(
		`UPDATE approval_requests
			SET status='rejected', decided_at=?, decision_source='web'
			WHERE id = ? AND status = 'pending' AND expires_at > ?`,
		now, req.ID, now,
	)
	if err != nil {
		renderApprovalResult(w, "error", "更新授權狀態失敗")
		return
	}

	// 檢查是否真的更新了（可能狀態已經不是 pending 或已過期）
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// 重新查詢狀態
		var currentStatus string
		_ = db.DB.QueryRow("SELECT status FROM approval_requests WHERE id = ?", req.ID).Scan(&currentStatus)
		if currentStatus == "expired" {
			renderApprovalResult(w, "expired", "授權已過期，請連絡車廠")
		} else {
			renderApprovalResult(w, "decided", "此授權已決定")
		}
		return
	}

	log.Printf("授權已拒絕: work_order_id=%d", req.WorkOrderID)
	renderApprovalResult(w, "success", "您的拒絕已送出，車廠會與您聯繫")
}

// renderApprovalPage 渲染授權頁面（簡單 HTML）
func renderApprovalPage(w http.ResponseWriter, data pages.ApprovalPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ApprovalPage(data).Render(context.Background(), w)
	return
	/*

			html := `<!DOCTYPE html>
		<html lang="zh-TW">
		<head>
		    <meta charset="UTF-8">
		    <meta name="viewport" content="width=device-width, initial-scale=1.0">
		    <title>追加維修授權</title>
		    <style>
		        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #1a1a1a; color: #e0e0e0; margin: 0; padding: 20px; }
		        .container { max-width: 600px; margin: 0 auto; background: #2d2d2d; border-radius: 8px; padding: 30px; }
		        h1 { color: #4a9eff; margin-top: 0; }
		        .error { color: #ff6b6b; background: #3d1f1f; padding: 15px; border-radius: 4px; }
		        .success { color: #51cf66; background: #1f3d2f; padding: 15px; border-radius: 4px; }
		        .info { background: #1f2d3d; padding: 15px; border-radius: 4px; margin: 15px 0; }
		        .btn { display: inline-block; padding: 12px 24px; margin: 10px 5px; border: none; border-radius: 4px; cursor: pointer; font-size: 16px; text-decoration: none; }
		        .btn-approve { background: #51cf66; color: white; }
		        .btn-reject { background: #ff6b6b; color: white; }
		        .btn:hover { opacity: 0.9; }
		        .item { border-bottom: 1px solid #3d3d3d; padding: 10px 0; }
		        .item:last-child { border-bottom: none; }
		    </style>
		</head>
		<body>
		    <div class="container">
		        <h1>🔧 追加維修授權</h1>`

			if data.ErrorMsg != "" {
				html += fmt.Sprintf(`<div class="error">%s</div>`, data.ErrorMsg)
			} else if data.IsExpired {
				html += `<div class="error">授權已過期，請連絡車廠</div>`
			} else if data.IsDecided {
				html += fmt.Sprintf(`<div class="success">此授權%s（%s）</div>`, data.DecidedText, data.Request.DecidedAt)
			} else if data.Request != nil && data.WorkOrder != nil {
				html += fmt.Sprintf(`
		        <div class="info">
		            <strong>工單編號：</strong>#%d<br>
		            <strong>客戶：</strong>%s<br>
		            <strong>車牌：</strong>%s<br>
		            <strong>過期時間：</strong>%s
		        </div>`,
					data.WorkOrder.ID, data.Customer.Name, data.WorkOrder.PlateNumber, data.Request.ExpiresAt)

				if data.Request.ItemSummary != "" {
					html += fmt.Sprintf(`
		        <div class="info">
		            <strong>追加項目：</strong><br>
		            %s
		        </div>`, data.Request.ItemSummary)
				}

				if len(data.Items) > 0 {
					html += `<div class="info"><strong>工單項目：</strong><br>`
					for _, item := range data.Items {
						html += fmt.Sprintf(`<div class="item">%s x%d - $%.0f</div>`,
							item.Description, item.Quantity, item.Subtotal)
					}
					html += `</div>`
				}

				html += `
		        <form method="POST" action="/approval/approve/` + data.Request.Token + `">
		            <button type="submit" class="btn btn-approve">✅ 同意</button>
		        </form>
		        <form method="POST" action="/approval/reject/` + data.Request.Token + `">
		            <button type="submit" class="btn btn-reject">❌ 拒絕</button>
		        </form>`
			}

			html += `
		    </div>
		</body>
		</html>`

			w.Write([]byte(html))
	*/
}

// renderApprovalResult 渲染授權結果頁面
func renderApprovalResult(w http.ResponseWriter, status, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ApprovalResult(status, message).Render(context.Background(), w)
	return
	/*

			var cssClass string
			switch status {
			case "success":
				cssClass = "success"
			case "error", "expired", "decided":
				cssClass = "error"
			default:
				cssClass = "info"
			}

			html := fmt.Sprintf(`<!DOCTYPE html>
		<html lang="zh-TW">
		<head>
		    <meta charset="UTF-8">
		    <meta name="viewport" content="width=device-width, initial-scale=1.0">
		    <title>授權結果</title>
		    <style>
		        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #1a1a1a; color: #e0e0e0; margin: 0; padding: 20px; }
		        .container { max-width: 600px; margin: 0 auto; background: #2d2d2d; border-radius: 8px; padding: 30px; }
		        h1 { color: #4a9eff; margin-top: 0; }
		        .%s { padding: 15px; border-radius: 4px; margin: 20px 0; }
		        .success { color: #51cf66; background: #1f3d2f; }
		        .error { color: #ff6b6b; background: #3d1f1f; }
		        .info { color: #4a9eff; background: #1f2d3d; }
		    </style>
		</head>
		<body>
		    <div class="container">
		        <h1>🔧 授權結果</h1>
		        <div class="%s">%s</div>
		        <p>您可以關閉此頁面。</p>
		    </div>
		</body>
		</html>`, cssClass, cssClass, message)

			w.Write([]byte(html))
	*/
}

// getWorkOrder 取得工單資訊（簡化版，只用於授權頁面）
func getWorkOrder(id int64) (*model.WorkOrder, error) {
	var wo model.WorkOrder
	err := db.DB.QueryRow(
		`SELECT id, vehicle_id, customer_id, current_mileage, status, approval_status,
			COALESCE(total_amount,0), created_at
			FROM active_work_orders WHERE id = ?`,
		id,
	).Scan(
		&wo.ID, &wo.VehicleID, &wo.CustomerID, &wo.CurrentMileage,
		&wo.Status, &wo.ApprovalStatus, &wo.TotalAmount, &wo.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	// 取得車牌號碼
	err = db.DB.QueryRow(
		"SELECT plate_number FROM active_vehicles WHERE id = ?",
		wo.VehicleID,
	).Scan(&wo.PlateNumber)
	if err != nil {
		wo.PlateNumber = "未知車牌"
	}

	return &wo, nil
}

// getCustomer 取得客戶資訊（簡化版，只用於授權頁面）
func getCustomer(id int64) (*model.Customer, error) {
	var c model.Customer
	err := db.DB.QueryRow(
		"SELECT id, name, phone FROM active_customers WHERE id = ?",
		id,
	).Scan(&c.ID, &c.Name, &c.Phone)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// getWorkOrderItems 取得工單項目（簡化版，只用於授權頁面）
func getWorkOrderItems(woID int64) ([]model.WorkOrderItem, error) {
	rows, err := db.DB.Query(
		`SELECT id, item_type, description, quantity, unit_price, subtotal
			FROM work_order_items WHERE work_order_id = ?`,
		woID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.WorkOrderItem
	for rows.Next() {
		var item model.WorkOrderItem
		err := rows.Scan(
			&item.ID, &item.ItemType, &item.Description,
			&item.Quantity, &item.UnitPrice, &item.Subtotal,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
