package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/service"
	"autorepairos/web/pages"
)

const inventoryCols = `i.id, COALESCE(i.supplier_id,0), COALESCE(s.name,''), i.part_number,
	COALESCE(i.barcode,''), i.name, i.bin_location, COALESCE(i.cost_price,0),
	COALESCE(i.selling_price,0), COALESCE(i.stock_quantity,0), COALESCE(i.min_safety_stock,0),
	COALESCE(i.category,''), COALESCE(i.item_kind,'part')`

func scanInventoryItem(rows interface{ Scan(...interface{}) error }) (model.InventoryItem, error) {
	var it model.InventoryItem
	err := rows.Scan(&it.ID, &it.SupplierID, &it.SupplierName, &it.PartNumber, &it.Barcode,
		&it.Name, &it.BinLocation, &it.CostPrice, &it.SellingPrice,
		&it.StockQuantity, &it.MinSafetyStock, &it.Category, &it.ItemKind)
	it.LowStock = it.StockQuantity < it.MinSafetyStock
	return it, err
}

func CreateInventoryItem(w http.ResponseWriter, r *http.Request) {
	part := strings.TrimSpace(r.FormValue("part_number"))
	name := strings.TrimSpace(r.FormValue("name"))
	bin := strings.TrimSpace(r.FormValue("bin_location"))
	category := strings.TrimSpace(r.FormValue("category"))

	rejectInventory := func(errMsg string) {
		user, _ := currentUser(r)
		w.WriteHeader(http.StatusBadRequest)
		renderInventoryList(w, r, user, pages.InventoryCreateForm{
			ErrMsg: errMsg, Part: part, Name: name, Bin: bin,
			Barcode:   strings.TrimSpace(r.FormValue("barcode")),
			Cost:      strings.TrimSpace(r.FormValue("cost_price")),
			Selling:   strings.TrimSpace(r.FormValue("selling_price")),
			Stock:     strings.TrimSpace(r.FormValue("stock_quantity")),
			MinSafety: strings.TrimSpace(r.FormValue("min_safety_stock")),
		})
	}
	if part == "" || name == "" || bin == "" {
		rejectInventory("料號、名稱、庫位為必填")
		return
	}

	var supArg interface{}
	if s := mustInt(r.FormValue("supplier_id")); s > 0 {
		supArg = s
	}

	_, err := db.DB.Exec(`INSERT INTO inventory_items
		(supplier_id, part_number, barcode, name, bin_location, category, cost_price, selling_price,
		 stock_quantity, min_safety_stock)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		supArg, part, nullStr(r.FormValue("barcode")), name, bin, nullStr(category),
		atofDefault(r.FormValue("cost_price"), 0), atofDefault(r.FormValue("selling_price"), 0),
		atoiDefault(r.FormValue("stock_quantity"), 0), atoiDefault(r.FormValue("min_safety_stock"), 5))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "料號已存在")
			return
		}
		fail(w, "新增零件失敗")
		return
	}

	redirectMsg(w, r, "/inventory", "零件已新增")
}

func EditInventoryForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pages.EditInventoryPage(it, canSeeMoney(user), lookupOptions("category", it.Category), supplierOptionsSel(it.SupplierID), user).Render(r.Context(), w)
}

func UpdateInventoryItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	part := strings.TrimSpace(r.FormValue("part_number"))
	name := strings.TrimSpace(r.FormValue("name"))
	bin := strings.TrimSpace(r.FormValue("bin_location"))

	if part == "" || name == "" || bin == "" {
		formError(w, r, "編輯零件", "/inventory/"+strconv.FormatInt(id, 10)+"/edit", "料號、名稱、庫位為必填")
		return
	}

	var supArg interface{}
	if s := mustInt(r.FormValue("supplier_id")); s > 0 {
		supArg = s
	}

	res, err := db.DB.Exec(`UPDATE inventory_items SET supplier_id=?, part_number=?, barcode=?,
		name=?, category=?, bin_location=?, cost_price=?, selling_price=?, min_safety_stock=? WHERE id=?`,
		supArg, part, nullStr(r.FormValue("barcode")), name, nullStr(r.FormValue("category")), bin,
		atofDefault(r.FormValue("cost_price"), 0), atofDefault(r.FormValue("selling_price"), 0),
		atoiDefault(r.FormValue("min_safety_stock"), 5), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "料號已存在")
			return
		}
		fail(w, "更新零件失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/inventory", "零件已更新")
}

func StockAdjustForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pages.StockAdjustPage(it, user).Render(r.Context(), w)
}

func ApplyStockAdjust(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	delta := atoiDefault(r.FormValue("delta"), 0)

	if delta == 0 {
		formError(w, r, "庫存調整", "/inventory/"+strconv.FormatInt(id, 10)+"/stock", "異動數量不可為 0")
		return
	}
	if delta == -int(^uint(0)>>1)-1 {
		formError(w, r, "庫存調整", "/inventory/"+strconv.FormatInt(id, 10)+"/stock", "異動數量超出範圍")
		return
	}

	reason := strings.TrimSpace(r.FormValue("note"))
	if reason == "" {
		reason = "手動調庫"
	}
	actorID := int64(0)
	if u, uerr := currentUser(r); uerr == nil {
		actorID = u.ID
	}

	after, err := service.NewInventoryService(db.DB).ApplyStockDelta(r.Context(), int(id), delta, reason, "manual", strconv.FormatInt(actorID, 10))
	if err != nil {
		var short *service.StockShortfall
		if errors.As(err, &short) {
			fail(w, "庫存不足：目前 "+strconv.Itoa(short.Current)+"，無法扣 "+strconv.Itoa(short.Needed)+
				"（若是舊系統留下的負庫存，請用「負庫存補正」）")
			return
		}
		if errors.Is(err, service.ErrItemNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, service.ErrMovementWrite) {
			log.Printf("寫入庫存異動失敗: %v", err)
			fail(w, "寫入異動紀錄失敗")
			return
		}
		fail(w, "調庫失敗")
		return
	}

	redirectMsg(w, r, "/inventory", "庫存已調整 "+strconv.Itoa(delta)+"，目前 "+strconv.Itoa(after))
}

func DeleteInventoryItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	used := countRows("SELECT COUNT(1) FROM work_order_items WHERE inventory_item_id = ?", id)
	if used > 0 {
		fail(w, "此零件已被工單引用，無法刪除")
		return
	}

	res, err := db.DB.Exec("UPDATE inventory_items SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除零件失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/inventory", "零件已刪除")
}
