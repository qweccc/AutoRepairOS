package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// 代码对照表维护。
//
// 为什么需要：
//   旧系统迁移过来的资料使用代码（如保险 company_no='07'、部门 branch='A'），
//   但新系统没有统一的代码对照表，导致投保纪录显示「07」而非「富邦」、
//   部门名称硬编码在 Go 代码里、银行/零件类别/险种等自由文字无法统计。
//
// 设计：
//   统一的 lookup_items 表，用 category 区分不同类型，
//   所有代码对照都在一个页面维护。

var lookupCategoryLabel = map[string]string{
	"bank":                 "銀行",
	"category":             "零件類別",
	"insurance_class":      "保險險種",
	"branch":               "部門",
	"vehicle_model":        "車輛品牌車型",
	"vehicle_type":         "車輛車種",
	"body_style":           "車身型式",
	"fuel_type":            "燃油種類",
	"bay":                  "工位",
	"walkaround_mark_type": "車況標記類型",
}

// CheckLookupCategories 啟動時檢查每個分頁籤的 category 是否真的有資料。
//
// 為什麼需要：新增一個 lookup category 要動兩處 —— migration 灌資料 +
// lookupCategoryLabel 加標籤。只做一半就會出現「半成品」：
//   - 有標籤沒資料 → 分頁籤點進去是空白（曾發生在 insurance_company）
//   - 有資料沒標籤 → 資料進得去卻沒有維護入口（曾發生在 vehicle_type）
//
// 兩種都不會報錯，只會讓人以為功能壞了，很難查到原因。
//
// 這個檢查只 log 警告、不會中斷啟動：空的 category 不該讓服務起不來，
// 但要讓它出現在 log 裡能被發現。
func CheckLookupCategories() {
	for cat, label := range lookupCategoryLabel {
		if countRows(`SELECT COUNT(1) FROM lookup_items WHERE category = ?`, cat) == 0 {
			log.Printf("[lookup] 警告：分頁籤「%s」（category=%s）沒有任何資料。"+
				"新增 lookup category 需要同時做兩件事：migration 灌資料 + "+
				"lookupCategoryLabel 加標籤，目前只有後者。此分頁會顯示空白。",
				label, cat)
		}
	}

	// 反向檢查：DB 有資料但沒有分頁籤 → 沒有維護入口。
	rows, err := db.DB.Query(`SELECT DISTINCT category FROM lookup_items`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		if rows.Scan(&cat) != nil {
			continue
		}
		if _, ok := lookupCategoryLabel[cat]; !ok {
			log.Printf("[lookup] 警告：資料表有 category=%s 的資料，但 lookupCategoryLabel "+
				"沒有對應標籤，代碼對照頁因此沒有維護入口。", cat)
		}
	}
}

// ListLookupItems 代码对照表列表 + 新增/编辑。
func ListLookupItems(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	category := strings.TrimSpace(qv.Get("category"))
	if category == "" {
		category = "branch"
	}

	// 筛选
	where := ` WHERE category = ?`
	var args []interface{}
	args = append(args, category)

	total := countRows(`SELECT COUNT(1) FROM lookup_items`+where, args...)

	query := `SELECT id, category, code, label, sort_order, active
		FROM lookup_items` + where + ` ORDER BY sort_order, code`
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		fail(w, "查詢代碼對照表失敗")
		return
	}
	defer rows.Close()

	var list []model.LookupItem
	for rows.Next() {
		var li model.LookupItem
		if err := rows.Scan(&li.ID, &li.Category, &li.Code, &li.Label, &li.SortOrder, &li.Active); err != nil {
			continue
		}
		list = append(list, li)
	}

	var lookupRows []pages.LookupRow
	for _, li := range list {
		lookupRows = append(lookupRows, pages.LookupRow{
			ID:        li.ID,
			Code:      li.Code,
			Label:     li.Label,
			SortOrder: li.SortOrder,
			Active:    li.Active,
		})
	}

	// lookupCategoryOrder 固定分頁籤順序（map 迭代是隨機的，導致每頁順序不同）。
	var lookupCategoryOrder = []string{
		"vehicle_type", "body_style", "fuel_type", "bank", "category",
		"insurance_class", "branch", "vehicle_model", "bay", "walkaround_mark_type",
	}

	// 分类筛选签
	var tabs strings.Builder
	for _, key := range lookupCategoryOrder {
		label, ok := lookupCategoryLabel[key]
		if !ok {
			continue
		}
		cls := "btn-s"
		if category == key {
			cls = "btn-p"
		}
		tabs.WriteString(`<a class="btn ` + cls + `" href="/lookup?category=` + key + `">` + label + `</a> `)
	}

	pages.ListLookupPage(lookupRows, category, lookupCategoryLabel[category], total,
		tabs.String(),
		user).Render(r.Context(), w)
}

// SearchLookupAPI 代碼對照即時搜尋 API（HTMX 用）。
func SearchLookupAPI(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if category == "" {
		category = "branch"
	}
	where := ` WHERE category = ?`
	args := []interface{}{category}
	if q != "" {
		where += ` AND (code LIKE ? OR label LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	rows, err := db.DB.Query(`SELECT id, category, code, label, sort_order, active
		FROM lookup_items`+where+` ORDER BY sort_order, code LIMIT 100`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.LookupRow
	for rows.Next() {
		var li pages.LookupRow
		var cat string
		if err := rows.Scan(&li.ID, &cat, &li.Code, &li.Label, &li.SortOrder, &li.Active); err != nil {
			continue
		}
		result = append(result, li)
	}
	pages.LookupSearchRows(result).Render(r.Context(), w)
}

// NewLookupItemForm 新增代码对照表单项表单。
func NewLookupItemForm(w http.ResponseWriter, r *http.Request, user model.User) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		category = "branch"
	}
	pages.NewLookupItemPage(category, lookupCategoryLabel[category], user).Render(r.Context(), w)
}

// CreateLookupItem 新增代码对照表单项。
func CreateLookupItem(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.FormValue("category"))
	code := strings.TrimSpace(r.FormValue("code"))
	label := strings.TrimSpace(r.FormValue("label"))
	if category == "" || code == "" || label == "" {
		formError(w, r, "新增代碼對照", "/lookup", "分類、代碼、名稱為必填")
		return
	}
	_, err := db.DB.Exec(`INSERT INTO lookup_items (category, code, label, sort_order) VALUES (?,?,?,?)`,
		category, code, label, atoiDefault(r.FormValue("sort_order"), 0))
	if err != nil {
		fail(w, "新增失敗")
		return
	}
	redirectMsg(w, r, "/lookup?category="+category, "代碼已新增")
}

// EditLookupItemForm 编辑代码对照表单项表单。
func EditLookupItemForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	var li model.LookupItem
	err := db.DB.QueryRow(`SELECT id, category, code, label, sort_order, active FROM lookup_items WHERE id = ?`, id).
		Scan(&li.ID, &li.Category, &li.Code, &li.Label, &li.SortOrder, &li.Active)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pages.EditLookupItemPage(li, user).Render(r.Context(), w)
}

// UpdateLookupItem 更新代码对照表单项。
func UpdateLookupItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	code := strings.TrimSpace(r.FormValue("code"))
	label := strings.TrimSpace(r.FormValue("label"))
	if code == "" || label == "" {
		formError(w, r, "編輯代碼對照", "/lookup/"+strconv.FormatInt(id, 10)+"/edit", "代碼、名稱為必填")
		return
	}
	var category string
	db.DB.QueryRow(`SELECT category FROM lookup_items WHERE id = ?`, id).Scan(&category)
	_, err := db.DB.Exec(`UPDATE lookup_items SET code=?, label=?, sort_order=? WHERE id=?`,
		code, label, atoiDefault(r.FormValue("sort_order"), 0), id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}
	redirectMsg(w, r, "/lookup?category="+category, "代碼已更新")
}

// DeactivateLookupItem 停用代码对照表单项。
func DeactivateLookupItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	var category string
	db.DB.QueryRow(`SELECT category FROM lookup_items WHERE id = ?`, id).Scan(&category)
	db.DB.Exec(`UPDATE lookup_items SET active = 0 WHERE id = ?`, id)
	redirectMsg(w, r, "/lookup?category="+category, "代碼已停用")
}

// ReactivateLookupItem 复用代码对照表单项。
func ReactivateLookupItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	var category string
	db.DB.QueryRow(`SELECT category FROM lookup_items WHERE id = ?`, id).Scan(&category)
	db.DB.Exec(`UPDATE lookup_items SET active = 1 WHERE id = ?`, id)
	redirectMsg(w, r, "/lookup?category="+category, "代碼已復用")
}

// lookupLabel 查代码对照表的名称。
// 找不到时回传代码本身（降级显示）。
func lookupLabel(category, code string) string {
	if code == "" {
		return ""
	}
	var label string
	err := db.DB.QueryRow(`SELECT label FROM lookup_items WHERE category = ? AND code = ? AND active = 1`, category, code).Scan(&label)
	if err != nil {
		return code
	}
	return label
}

// lookupOptions 产生代码对照表的下拉选单 HTML。
func lookupOptions(category, selected string) string {
	rows, err := db.DB.Query(`SELECT code, label FROM lookup_items WHERE category = ? AND active = 1 ORDER BY sort_order, code`, category)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var code, label string
		if rows.Scan(&code, &label) != nil {
			continue
		}
		sel := ""
		if code == selected {
			sel = " selected"
		}
		b.WriteString(`<option value="` + htmlEsc(code) + `"` + sel + `>` + htmlEsc(label) + `</option>`)
	}
	return b.String()
}

// 保險公司刻意不在這個對照表裡。
//
// 它有自己的完整維護頁 /insurance-companies（代碼、公司名稱、統編、客服電話、
// 聯絡人、地址、停用），欄位遠比 lookup 的 code/label 兩欄豐富。放進來會變成
// 兩份真相：在保險公司頁改了名字，lookup 這份不會同步，而保單與工單都參照
// insurance_companies.id。
//
// 曾經在 lookupCategoryLabel 裡掛了一個 insurance_company 分頁籤，但沒有任何
// migration 灌過這個 category 的資料，點進去只會看到「共 0 筆」的空頁。
