// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ============================================================
// 保養套餐範本的 CRUD（maintenance_templates + maintenance_template_items）
//
// 原本只有唯讀列表（舊系統遷入的 21 個里程級距），導致車行無法建立
// 自己的標準工項（例如本店「小保養」的固定項目組合），每次開單都要手動加。
//
// 設計要點：
//   - 套餐本體與項目分開存，項目可整批重寫（表單一次送出全部列）。
//     不用「逐項增刪」是因為編輯時最常見的操作是調整順序與單價，
//     整批重寫在 UI 上更直覺，也不會有「漏刪一項」的殘留。
//   - 舊系統 code（A0002 / G000 …）是 UNIQUE 且舊工單可能參照，建立後不可改。
//     要換代碼請新建並停用舊的。
//   - 沒有 active 欄位（舊表沒有），因此「停用」用刪除表達——套餐是範本不是
//     憑證，刪除不影響已開出的工單（工單項目是快照，不參照範本）。
// ============================================================

// NewMaintenanceTemplateForm 新增保養套餐。
func NewMaintenanceTemplateForm(w http.ResponseWriter, r *http.Request, user model.User) {
	body := `
    <div class="card">
      <h2>新增保養套餐</h2>
      <form class="frm" method="POST" action="/maintenance-templates">
        <label>代碼<input name="code" required placeholder="例：LOCAL-5K（建立後不可修改）"></label>
        <label>套餐名稱<input name="name" required placeholder="例：5千公里定期保養"></label>
        <label class="full">備註<textarea name="notes" placeholder="適用車款、注意事項"></textarea></label>
        <div class="full">
          <button class="btn btn-p" type="submit">建立</button>
          <a class="btn btn-s" href="/maintenance-templates">取消</a>
        </div>
      </form>
      <p style="color:#64748b;font-size:12.5px;margin-top:10px">
        建立後可在編輯頁加入標準項目（料號、名稱、數量、單價）。代碼建立後不可修改，
        因為舊工單可能參照同一組編號。
      </p>
    </div>`

	renderPage(w, "新增保養套餐", "/maintenance-templates", user.Username, user.Role, body)
}

// CreateMaintenanceTemplate 建立保養套餐（只建本體，項目稍後在編輯頁補）。
func CreateMaintenanceTemplate(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.FormValue("code"))
	name := strings.TrimSpace(r.FormValue("name"))
	if code == "" || name == "" {
		fail(w, "代碼與名稱為必填")
		return
	}

	res, err := db.DB.Exec(`INSERT INTO maintenance_templates (code, name, notes) VALUES (?,?,?)`,
		code, name, nullStr(r.FormValue("notes")))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "代碼「"+code+"」已存在，請用其他代碼")
			return
		}
		fail(w, "建立保養套餐失敗")
		return
	}
	id, _ := res.LastInsertId()
	// 直接導向編輯頁，讓使用者立刻補項目——否則會建出一個空套餐卻無從下手。
	redirectMsg(w, r, "/maintenance-templates/"+strconv.FormatInt(id, 10)+"/edit",
		"套餐已建立，請加入標準項目")
}

// EditMaintenanceTemplateForm 編輯套餐與項目（整批重寫項目）。
func EditMaintenanceTemplateForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	var code, name, notes string
	if err := db.DB.QueryRow(`SELECT code, name, COALESCE(notes,'') FROM maintenance_templates WHERE id = ?`, id).
		Scan(&code, &name, &notes); err != nil {
		http.NotFound(w, r)
		return
	}

	rows, err := db.DB.Query(`SELECT i.sequence_no, COALESCE(i.part_number,''),
		COALESCE(inv.name, i.description, ''), i.quantity, i.unit_price
		FROM maintenance_template_items i
		LEFT JOIN inventory_items inv ON inv.part_number = i.part_number
		WHERE i.template_id = ? ORDER BY i.sequence_no, i.id`, id)
	if err != nil {
		fail(w, "查詢套餐項目失敗")
		return
	}
	defer rows.Close()

	var itemRows strings.Builder
	n := 0
	for rows.Next() {
		var seq int
		var part, desc string
		var qty, price float64
		if rows.Scan(&seq, &part, &desc, &qty, &price) != nil {
			continue
		}
		n++
		itemRows.WriteString(`<tr class="tpl-item">
            <td><input name="part_number" value="` + htmlEsc(part) + `" placeholder="料號（可空）"
                       style="width:120px;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>
            <td><input name="item_desc" value="` + htmlEsc(desc) + `" required
                       style="width:100%;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>
            <td><input name="item_qty" type="number" min="0" step="0.1" value="` + trimQty(qty) + `"
                       style="width:80px;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>
            <td><input name="item_price" type="number" min="0" step="1" value="` + money(price) + `"
                       style="width:100px;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>
            <td><button type="button" class="btn btn-d" onclick="this.closest('tr').remove()">移除</button></td>
          </tr>`)
	}

	body := `
    <div class="card">
      <h2>編輯保養套餐 ` + htmlEsc(code) + `</h2>
      <form class="frm" method="POST" action="/maintenance-templates/` + strconv.FormatInt(id, 10) + `">
        <label>代碼<input value="` + htmlEsc(code) + `" disabled style="opacity:0.6"></label>
        <label>套餐名稱<input name="name" value="` + htmlEsc(name) + `" required></label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(notes) + `</textarea></label>
      </form>

      <h3 style="margin:20px 0 8px;font-size:14px">標準項目</h3>
      <p style="color:#64748b;font-size:12.5px;margin:0 0 10px">
        儲存時會以這裡的內容整批取代原有項目。空白列會被忽略；全刪光即代表此套餐無標準項目。
      </p>
      <form method="POST" action="/maintenance-templates/` + strconv.FormatInt(id, 10) + `/items">
        <table style="width:100%">
          <tr><th>料號</th><th>名稱</th><th>數量</th><th>單價</th><th></th></tr>
          <tbody id="tplItems">` + itemRows.String() + `</tbody>
        </table>
        <div style="margin-top:10px;display:flex;gap:9px">
          <button type="button" class="btn btn-s" onclick="addTplRow()">＋ 加入一列</button>
          <button type="submit" class="btn btn-p">儲存項目</button>
        </div>
      </form>

      <div style="margin-top:18px;padding-top:14px;border-top:1px solid #334155">
        <a class="btn btn-p" href="/maintenance-templates">返回列表</a>
        <form method="POST" action="/maintenance-templates/` + strconv.FormatInt(id, 10) +
		`/delete" style="display:inline" onsubmit="return confirm('確定刪除套餐「` + htmlEsc(code) + `」？已開出的工單不受影響。')"><button class="btn btn-d" type="submit">刪除套餐</button></form>
      </div>
    </div>

    <script>
    function addTplRow(){
      var tb = document.getElementById('tplItems');
      var tr = document.createElement('tr');
      tr.className = 'tpl-item';
      tr.innerHTML = '<td><input name="part_number" placeholder="料號（可空）" style="width:120px;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>' +
        '<td><input name="item_desc" required style="width:100%;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>' +
        '<td><input name="item_qty" type="number" min="0" step="0.1" value="1" style="width:80px;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>' +
        '<td><input name="item_price" type="number" min="0" step="1" value="0" style="width:100px;padding:6px 8px;border-radius:6px;border:1px solid #334155;background:#0f172a;color:#e2e8f0"></td>' +
        '<td><button type="button" class="btn btn-d" onclick="this.closest(\'tr\').remove()">移除</button></td>';
      tb.appendChild(tr);
    }
    </script>`

	renderPage(w, "編輯保養套餐", "/maintenance-templates", user.Username, user.Role, body)
}

// UpdateMaintenanceTemplate 更新套餐名稱與備註（code 不可改）。
func UpdateMaintenanceTemplate(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "套餐名稱為必填")
		return
	}
	res, err := db.DB.Exec(`UPDATE maintenance_templates SET name=?, notes=? WHERE id=?`,
		name, nullStr(r.FormValue("notes")), id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/maintenance-templates/"+strconv.FormatInt(id, 10)+"/edit", "套餐已更新")
}

// ReplaceMaintenanceTemplateItems 整批重寫套餐項目。
//
// 為什麼整批取代而不是逐項增刪：編輯時最常見的操作是調整順序與單價，
// 整批重寫在 UI 上最直覺，也不會有「改到一半漏刪一項」的殘留。
// 空白列（名稱為空）直接忽略，讓使用者可以留著空列稍後再填。
func ReplaceMaintenanceTemplateItems(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM maintenance_templates WHERE id = ?", id) == 0 {
		http.NotFound(w, r)
		return
	}

	// 必須先 ParseForm：r.Form 在呼叫 ParseForm/ParseMultipartForm 之前是空的。
	// 用 r.FormValue() 會隱含解析，但直接取 r.Form[...] 不會——
	// 少了這行會拿到 0 個欄位，所有項目被靜默丟棄（回傳「已儲存（0 項）」）。
	if err := r.ParseForm(); err != nil {
		fail(w, "表單解析失敗")
		return
	}

	parts := r.Form["part_number"]
	descs := r.Form["item_desc"]
	qtys := r.Form["item_qty"]
	prices := r.Form["item_price"]

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	// 先清空再重填。整批在同一個 transaction 內，失敗會整個回滾，
	// 不會出現「刪了舊的但新的沒寫入」的半套狀態。
	if _, err := tx.Exec("DELETE FROM maintenance_template_items WHERE template_id = ?", id); err != nil {
		fail(w, "重設項目失敗")
		return
	}

	seq := 0
	for i, desc := range descs {
		desc = strings.TrimSpace(desc)
		if desc == "" {
			continue
		}
		seq++
		part := ""
		if i < len(parts) {
			part = strings.TrimSpace(parts[i])
		}
		qty := 1.0
		if i < len(qtys) {
			if v := atofDefault(qtys[i], 1); v > 0 {
				qty = v
			}
		}
		price := 0.0
		if i < len(prices) {
			price = atofDefault(prices[i], 0)
		}
		if _, err := tx.Exec(`INSERT INTO maintenance_template_items
			(template_id, sequence_no, part_number, description, quantity, unit_price)
			VALUES (?,?,?,?,?,?)`, id, seq, nullStr(part), desc, qty, price); err != nil {
			fail(w, "寫入項目失敗")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/maintenance-templates/"+strconv.FormatInt(id, 10)+"/edit",
		"標準項目已儲存（"+strconv.Itoa(seq)+" 項）")
}

// DeleteMaintenanceTemplate 刪除套餐（連同項目）。
// 套餐是範本不是憑證，刪除不影響已開出的工單——工單項目是快照，不參照範本。
func DeleteMaintenanceTemplate(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM maintenance_template_items WHERE template_id = ?", id); err != nil {
		fail(w, "刪除項目失敗")
		return
	}
	res, err := tx.Exec("DELETE FROM maintenance_templates WHERE id = ?", id)
	if err != nil {
		fail(w, "刪除套餐失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/maintenance-templates", "保養套餐已刪除")
}

var _ = model.MaintenanceTemplate{}
