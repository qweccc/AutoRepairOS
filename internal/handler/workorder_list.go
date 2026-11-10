package handler

import (
	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func ListWorkOrders(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	status := qv.Get("status")
	q := strings.TrimSpace(qv.Get("q"))
	from := strings.TrimSpace(qv.Get("from"))
	to := strings.TrimSpace(qv.Get("to"))
	tech := strings.TrimSpace(qv.Get("tech")) // 技师筛选（users.id，'0' = 未指派）
	part := strings.TrimSpace(qv.Get("part")) // 零件筛选：料号或名称的文字查询
	mine := qv.Get("mine") == "1"             // 技师只看自己的工单
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if mine {
		where += ` AND wo.technician_id = ?`
		args = append(args, user.ID)
	}
	if status != "" {
		if _, ok := workOrderStatusLabel[status]; ok {
			where += ` AND wo.status = ?`
			args = append(args, status)
		}
	}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR c.phone LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	// 技師篩選。
	//   tech='0'     → 只看未指派（technician_id 為 0 或 NULL）
	//   tech='<id>'  → 該技師
	// 未指派刻意用獨立值而非留空：留空等於「不篩選」，
	// 而「找出漏派工的單」正是這頁最需要的查詢之一。
	if tech == "0" {
		where += ` AND COALESCE(wo.technician_id,0) = 0`
	} else if n := mustInt(tech); n > 0 {
		where += ` AND wo.technician_id = ?`
		args = append(args, n)
	}
	// 零件篩選：工單明細曾用過這個料號或名稱。
	//
	// 為什麼用文字而非下拉：料號有 10,366 個，做成 <select> 會讓這一頁
	// 從 13 KB 膨脹到 647 KB。用 LIKE 打「雨刷」「BE071」比捲動一萬項快。
	//
	// 用 EXISTS 而非 JOIN：同一張工單可能分多列用同一個料號
	// （實測 5,489 組，最多 10 列），JOIN 會讓該工單重複出現。
	if part != "" {
		where += ` AND EXISTS (SELECT 1 FROM work_order_items i
			LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
			WHERE i.work_order_id = wo.id
			  AND (ii.part_number LIKE ? OR ii.name LIKE ? OR i.description LIKE ?))`
		like := "%" + part + "%"
		args = append(args, like, like, like)
	}
	// 日期區間。
	//
	// 注意：SQLite 的 date() 不吃斜線格式（date('2016/02/22') 會回 NULL），
	// 而舊系統遷移進來的 created_at 正是 'YYYY/MM/DD'。
	// 因此先用 replace() 正規化成 '-' 再比對，兩種格式都能正確篩選。
	const woDate = `date(replace(wo.created_at,'/','-'))`
	if from != "" {
		where += ` AND ` + woDate + ` >= date(?)`
		args = append(args, from)
	}
	if to != "" {
		where += ` AND ` + woDate + ` <= date(?)`
		args = append(args, to)
	}

	// 總筆數（分頁用）
	total := 0
	if err := db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_orders wo
		JOIN active_vehicles v ON v.id = wo.vehicle_id
		JOIN active_customers c ON c.id = wo.customer_id`+where, args...).Scan(&total); err != nil {
		log.Printf("查詢工單總數失敗: %v", err)
		fail(w, "查詢工單總數失敗")
		return
	}

	query := `SELECT ` + workOrderCols + workOrderFrom + where +
		` ORDER BY wo.created_at DESC, wo.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢工單失敗")
		return
	}
	defer rows.Close()

	var list []model.WorkOrder
	for rows.Next() {
		wo, err := scanWorkOrder(rows)
		if err != nil {
			log.Printf("讀取工單列表失敗: %v", err)
			fail(w, "讀取工單列表失敗")
			return
		}
		list = append(list, wo)
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉工單列表失敗: %v", err)
		fail(w, "讀取工單列表失敗")
		return
	}
	rows.Close()

	// 零件欄：整頁一次查回，避免每列一次查詢（N+1）。
	// 同料號分多列的情況在這裡彙總（見 woPartUsage 的說明）。
	if len(list) > 0 {
		ids := make([]string, 0, len(list))
		idx := make(map[int64]int, len(list))
		for i, wo := range list {
			ids = append(ids, strconv.FormatInt(wo.ID, 10))
			idx[wo.ID] = i
		}
		q := `SELECT i.work_order_id, COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,''),
				COALESCE(SUM(i.quantity),0)
			FROM work_order_items i
			LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
			WHERE i.item_type = 'part' AND i.work_order_id IN (` + strings.Join(ids, ",") + `)
			GROUP BY i.work_order_id, COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,'')
			ORDER BY i.work_order_id, COALESCE(ii.part_number,'')`
		if pr, perr := db.DB.Query(q); perr == nil {
			for pr.Next() {
				var wid int64
				var part, name string
				var qty float64
				if err := pr.Scan(&wid, &part, &name, &qty); err != nil {
					log.Printf("讀取工單零件摘要失敗: %v", err)
					pr.Close()
					fail(w, "讀取工單零件摘要失敗")
					return
				}
				i, ok := idx[wid]
				if !ok {
					continue
				}
				if list[i].PartSummary != "" {
					list[i].PartSummary += "、"
				}
				list[i].PartSummary += partDisplayLabel(part, name) + "×" + trimQty(qty)
				list[i].PartCount++
			}
			if err := pr.Err(); err != nil {
				log.Printf("列舉工單零件摘要失敗: %v", err)
				pr.Close()
				fail(w, "讀取工單零件摘要失敗")
				return
			}
			pr.Close()
		} else {
			log.Printf("查詢工單零件摘要失敗: %v", perr)
			fail(w, "查詢工單零件摘要失敗")
			return
		}
	}

	var woRows []pages.WorkOrderRow
	for _, wo := range list {
		woRows = append(woRows, pages.WorkOrderRow{
			ID:             wo.ID,
			PlateNumber:    wo.PlateNumber,
			BrandModel:     wo.BrandModel,
			CustomerName:   wo.CustomerName,
			CustomerPhone:  wo.CustomerPhone,
			TechnicianName: wo.TechnicianName,
			Status:         wo.Status,
			StatusLabel:    workOrderStatusLabel[wo.Status],
			StatusPill:     statusPill[wo.Status],
			ApprovalStatus: wo.ApprovalStatus,
			ApprovalLabel:  approvalLabel[wo.ApprovalStatus],
			ApprovalPill:   approvalPill[wo.ApprovalStatus],
			IsInsurance:    wo.IsInsurance,
			PartSummary:    wo.PartSummary,
			PartCount:      wo.PartCount,
			CurrentMileage: wo.CurrentMileage,
			TotalAmount:    wo.TotalAmount,
			CreatedAt:      woCreatedText(wo.CreatedAt),
		})
	}

	// 狀態分頁籤（保留搜尋與日期條件）
	var tabs strings.Builder
	if user.Role == "technician" {
		mineURL := "/work-orders?mine=1"
		if q != "" {
			mineURL += "&q=" + url.QueryEscape(q)
		}
		tabs.WriteString(`<a href="` + mineURL + `" class="btn ` + btnIf(mine, "btn-p", "btn-s") + `">我的工單</a> `)
	}
	tabs.WriteString(`<a href="` + woTabURL(qv, "") + `" class="btn ` + btnIf(WorkOrderStatus(status) == "" && !mine, "btn-p", "btn-s") + `">全部</a> `)
	for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
		tabs.WriteString(`<a href="` + woTabURL(qv, s) + `" class="btn ` + btnIf(WorkOrderStatus(status) == WorkOrderStatus(s), "btn-p", "btn-s") + `">` +
			workOrderStatusLabel[s] + `</a> `)
	}

	pages.ListWorkOrdersPage(woRows, total, page, size,
		q, part, status, tech, from, to,
		tabs.String(), woTechFilterOptions(tech),
		user).Render(r.Context(), w)
}

func SearchWorkOrdersAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("error") == "1" {
		w.WriteHeader(http.StatusInternalServerError)
		pages.WorkOrderSearchError().Render(r.Context(), w)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	part := strings.TrimSpace(r.URL.Query().Get("part"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	tech := strings.TrimSpace(r.URL.Query().Get("tech"))

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR c.phone LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if status != "" {
		if _, ok := workOrderStatusLabel[status]; ok {
			where += ` AND wo.status = ?`
			args = append(args, status)
		}
	}
	if part != "" {
		where += ` AND EXISTS (SELECT 1 FROM work_order_items i
			LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
			WHERE i.work_order_id = wo.id
			  AND (ii.part_number LIKE ? OR ii.name LIKE ? OR i.description LIKE ?))`
		like := "%" + part + "%"
		args = append(args, like, like, like)
	}
	if tech == "0" {
		where += ` AND COALESCE(wo.technician_id,0) = 0`
	} else if n := mustInt(tech); n > 0 {
		where += ` AND wo.technician_id = ?`
		args = append(args, n)
	}

	rows, err := db.DB.Query(`SELECT `+workOrderCols+workOrderFrom+where+
		` ORDER BY wo.created_at DESC, wo.id DESC LIMIT 50`, args...)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		pages.WorkOrderSearchError().Render(r.Context(), w)
		return
	}
	defer rows.Close()

	var result []pages.WorkOrderRow
	for rows.Next() {
		wo, err := scanWorkOrder(rows)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			pages.WorkOrderSearchError().Render(r.Context(), w)
			return
		}
		result = append(result, pages.WorkOrderRow{
			ID:             wo.ID,
			PlateNumber:    wo.PlateNumber,
			BrandModel:     wo.BrandModel,
			CustomerName:   wo.CustomerName,
			CustomerPhone:  wo.CustomerPhone,
			TechnicianName: wo.TechnicianName,
			Status:         wo.Status,
			StatusLabel:    workOrderStatusLabel[wo.Status],
			StatusPill:     statusPill[wo.Status],
			ApprovalStatus: wo.ApprovalStatus,
			ApprovalLabel:  approvalLabel[wo.ApprovalStatus],
			ApprovalPill:   approvalPill[wo.ApprovalStatus],
			IsInsurance:    wo.IsInsurance,
			CurrentMileage: wo.CurrentMileage,
			TotalAmount:    wo.TotalAmount,
			CreatedAt:      woCreatedText(wo.CreatedAt),
		})
	}
	if err := rows.Err(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		pages.WorkOrderSearchError().Render(r.Context(), w)
		return
	}
	pages.WorkOrderSearchRows(result).Render(r.Context(), w)
}

// woCreatedText 工單日期顯示。
//
// 舊資料遷移進來的 created_at 是 'YYYY/MM/DD' 字串（SQLite 無嚴格型別，
// driver 會依欄位宣告轉成 time.Time）；新開的工單則是 DATETIME。
// 這裡統一輸出成 YYYY-MM-DD，無法解析時退回原字串。

func woCreatedText(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

// woTabURL 產生狀態分頁籤連結，保留 q / from / to / tech / part 條件。

func woTabURL(qv url.Values, status string) string {
	out := url.Values{}
	for _, k := range []string{"q", "from", "to", "tech", "part"} {
		if v := qv.Get(k); v != "" {
			out.Set(k, v)
		}
	}
	if status != "" {
		out.Set("status", status)
	}
	if s := out.Encode(); s != "" {
		return "/work-orders?" + s
	}
	return "/work-orders"
}

// trimQty 數量顯示：整數不帶小數點（2 而非 2.0），有小數才保留一位。
// 舊資料的 quantity 是 REAL（1.0、2.0…），直接印會出現一堆 .0。

func trimQty(q float64) string {
	if q == float64(int64(q)) {
		return strconv.FormatInt(int64(q), 10)
	}
	return strconv.FormatFloat(q, 'f', 1, 64)
}

func partDisplayLabel(part, name string) string {
	part = strings.TrimSpace(part)
	name = strings.TrimSpace(name)
	switch {
	case name != "" && part != "" && name != part:
		return name + " (" + part + ")"
	case name != "":
		return name
	default:
		return part
	}
}

// woPartUsage 回傳指定工單用過的零件彙總（料號/名稱/數量），供列表展開顯示。
//
// 為什麼要彙總：同一張工單可能分多列用同一個料號（實測 5,489 組，最多 10 列），
// 逐列顯示會讓「用了幾個」看起來比實際少。這裡 SUM(quantity) 後才對。

func woPartUsage(workOrderID int64) []struct {
	Part string
	Name string
	Qty  float64
} {
	rows, err := db.DB.Query(`SELECT COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,''),
			COALESCE(SUM(i.quantity),0)
		FROM work_order_items i
		LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
		WHERE i.work_order_id = ? AND i.item_type = 'part'
		GROUP BY COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,'')
		ORDER BY COALESCE(ii.part_number,'')`, workOrderID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []struct {
		Part string
		Name string
		Qty  float64
	}
	for rows.Next() {
		var r struct {
			Part string
			Name string
			Qty  float64
		}
		if err := rows.Scan(&r.Part, &r.Name, &r.Qty); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}
