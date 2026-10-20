package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/service"
	"autorepairos/web/pages"
)

func ListEstimates(w http.ResponseWriter, r *http.Request, user model.User) {
	rows, err := db.DB.Query(`SELECT e.id, c.name, v.plate_number, v.brand_model, e.status,
		e.total_amount, e.discount_amount, e.created_at, COALESCE(e.converted_work_order_id,0)
		FROM estimates e JOIN customers c ON c.id=e.customer_id
		JOIN vehicles v ON v.id=e.vehicle_id
		ORDER BY e.created_at DESC, e.id DESC LIMIT 200`)
	if err != nil {
		fail(w, "查詢估價草稿失敗")
		return
	}
	defer rows.Close()
	var estimates []pages.EstimateListRow
	for rows.Next() {
		var e pages.EstimateListRow
		if err := rows.Scan(&e.ID, &e.CustomerName, &e.PlateNumber, &e.BrandModel,
			&e.Status, &e.TotalAmount, &e.DiscountAmount, &e.CreatedAt, &e.ConvertedWorkOrderID); err != nil {
			fail(w, "讀取估價草稿失敗")
			return
		}
		estimates = append(estimates, e)
	}
	if err := rows.Err(); err != nil {
		fail(w, "讀取估價草稿失敗")
		return
	}
	pages.EstimateListPage(estimates, user.Username, user.Role).Render(r.Context(), w)
}

func NewEstimate(w http.ResponseWriter, r *http.Request, user model.User) {
	rows, err := db.DB.Query(`SELECT v.id, v.plate_number, v.brand_model, c.name
		FROM active_vehicles v JOIN active_customers c ON c.id=v.customer_id
		ORDER BY v.plate_number LIMIT 1000`)
	if err != nil {
		fail(w, "載入車輛資料失敗")
		return
	}
	defer rows.Close()
	var vehicles []pages.EstimateVehicleOption
	for rows.Next() {
		var v pages.EstimateVehicleOption
		if err := rows.Scan(&v.ID, &v.PlateNumber, &v.BrandModel, &v.CustomerName); err != nil {
			fail(w, "讀取車輛資料失敗")
			return
		}
		vehicles = append(vehicles, v)
	}
	if err := rows.Err(); err != nil {
		fail(w, "讀取車輛資料失敗")
		return
	}
	pages.EstimateNewPage(vehicles, user.Username, user.Role).Render(r.Context(), w)
}

func CreateEstimate(w http.ResponseWriter, r *http.Request) {
	vehicleID := mustInt(r.FormValue("vehicle_id"))
	if vehicleID <= 0 {
		plate := strings.TrimSpace(r.FormValue("plate_number"))
		if plate != "" {
			err := db.DB.QueryRow(`SELECT id FROM active_vehicles
				WHERE UPPER(plate_number)=UPPER(?)`, plate).Scan(&vehicleID)
			if errors.Is(err, sql.ErrNoRows) {
				fail(w, "找不到這個車牌，請先建立車輛資料")
				return
			}
			if err != nil {
				fail(w, "查詢車輛資料失敗")
				return
			}
		}
	}
	user, err := currentUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	mileage := atoiDefault(r.FormValue("current_mileage"), 0)
	id, err := service.New(db.DB).CreateEstimate(r.Context(), service.EstimateInput{
		VehicleID: vehicleID, CurrentMileage: mileage,
		CustomerComplaint: r.FormValue("customer_complaint"), CreatedBy: user.ID,
	})
	if err != nil {
		fail(w, "建立估價草稿失敗："+err.Error())
		return
	}
	http.Redirect(w, r, "/estimates/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func ShowEstimate(w http.ResponseWriter, r *http.Request, user model.User) {
	estimateID := mustInt(r.PathValue("id"))
	e, items, err := loadEstimate(estimateID)
	if err != nil {
		if errors.Is(err, service.ErrEstimateNotFound) {
			http.NotFound(w, r)
			return
		}
		fail(w, "讀取估價草稿失敗")
		return
	}
	pages.EstimateDetailPage(e, items, user.Username, user.Role,
		user.Role == "owner" || user.Role == "counter").Render(r.Context(), w)
}

func AddEstimateItem(w http.ResponseWriter, r *http.Request) {
	estimateID := mustInt(r.PathValue("id"))
	input := service.EstimateItemInput{
		ItemType: r.FormValue("item_type"), Description: r.FormValue("description"),
		Quantity:  atoiDefault(r.FormValue("quantity"), 0),
		UnitPrice: atofDefault(r.FormValue("unit_price"), -1),
	}
	if err := service.New(db.DB).AddEstimateItem(r.Context(), estimateID, input); err != nil {
		fail(w, "新增估價項目失敗："+err.Error())
		return
	}
	http.Redirect(w, r, "/estimates/"+strconv.FormatInt(estimateID, 10), http.StatusSeeOther)
}

func DeleteEstimateItem(w http.ResponseWriter, r *http.Request) {
	estimateID, itemID := mustInt(r.PathValue("id")), mustInt(r.PathValue("item_id"))
	if err := service.New(db.DB).DeleteEstimateItem(r.Context(), estimateID, itemID); err != nil {
		fail(w, "刪除估價項目失敗："+err.Error())
		return
	}
	http.Redirect(w, r, "/estimates/"+strconv.FormatInt(estimateID, 10), http.StatusSeeOther)
}

func SetEstimateDiscount(w http.ResponseWriter, r *http.Request) {
	estimateID := mustInt(r.PathValue("id"))
	discount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("discount_amount")), 64)
	if err != nil {
		fail(w, "折扣金額格式錯誤")
		return
	}
	if err := service.New(db.DB).SetEstimateDiscount(r.Context(), estimateID, discount); err != nil {
		fail(w, "更新折扣失敗："+err.Error())
		return
	}
	http.Redirect(w, r, "/estimates/"+strconv.FormatInt(estimateID, 10), http.StatusSeeOther)
}

func ConvertEstimate(w http.ResponseWriter, r *http.Request) {
	estimateID := mustInt(r.PathValue("id"))
	workOrderID, err := service.New(db.DB).ConvertEstimate(r.Context(), estimateID)
	if err != nil {
		if errors.Is(err, service.ErrEstimateNotFound) {
			http.NotFound(w, r)
			return
		}
		fail(w, "轉換正式工單失敗："+err.Error())
		return
	}
	http.Redirect(w, r, "/work-orders/"+strconv.FormatInt(workOrderID, 10), http.StatusSeeOther)
}

func ShowWorkOrderEstimateSource(w http.ResponseWriter, r *http.Request, user model.User) {
	workOrderID := mustInt(r.PathValue("id"))
	var estimateID int64
	var quoteTotal, quoteDiscount float64
	err := db.DB.QueryRow(`SELECT original_estimate_id, quoted_total_amount,
		quoted_discount_amount FROM work_orders WHERE id = ? AND original_estimate_id IS NOT NULL`,
		workOrderID).Scan(&estimateID, &quoteTotal, &quoteDiscount)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(w, "查詢工單估價來源失敗")
		return
	}
	e, items, err := loadEstimate(estimateID)
	if err != nil {
		fail(w, "讀取原始估價失敗")
		return
	}
	pages.EstimateSourcePage(e, items, workOrderID, quoteTotal, quoteDiscount,
		user.Username, user.Role).Render(r.Context(), w)
}

func loadEstimate(id int64) (pages.EstimateDetail, []pages.EstimateItemRow, error) {
	var e pages.EstimateDetail
	err := db.DB.QueryRow(`SELECT e.id, c.name, c.phone, v.plate_number, v.brand_model,
		e.status, e.current_mileage, e.customer_complaint, e.discount_amount,
		e.total_amount, e.created_at, COALESCE(e.converted_work_order_id,0)
		FROM estimates e JOIN customers c ON c.id=e.customer_id
		JOIN vehicles v ON v.id=e.vehicle_id WHERE e.id = ?`, id).Scan(
		&e.ID, &e.CustomerName, &e.CustomerPhone, &e.PlateNumber, &e.BrandModel,
		&e.Status, &e.CurrentMileage, &e.CustomerComplaint, &e.DiscountAmount,
		&e.TotalAmount, &e.CreatedAt, &e.ConvertedWorkOrderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return e, nil, service.ErrEstimateNotFound
		}
		return e, nil, err
	}
	rows, err := db.DB.Query(`SELECT id, item_type, description, quantity, unit_price, subtotal
		FROM estimate_items WHERE estimate_id = ? ORDER BY id`, id)
	if err != nil {
		return e, nil, err
	}
	defer rows.Close()
	var items []pages.EstimateItemRow
	for rows.Next() {
		var it pages.EstimateItemRow
		if err := rows.Scan(&it.ID, &it.ItemType, &it.Description, &it.Quantity,
			&it.UnitPrice, &it.Subtotal); err != nil {
			return e, nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return e, nil, err
	}
	return e, items, nil
}
