package handler

import (
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// setupTechEfficiencyTestDB 建立技師效率分析測試所需的最小 schema。
func setupTechEfficiencyTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	statements := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY, username TEXT, name TEXT, role TEXT, active INTEGER
		)`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY, status TEXT, created_at TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE technician_worklogs (
			id INTEGER PRIMARY KEY, work_order_id INTEGER, technician_id INTEGER,
			work_date TEXT, hours REAL, hourly_rate REAL, labor_amount REAL,
			commission_rate REAL, commission_amount REAL, notes TEXT, deleted_at TEXT
		)`,
		`CREATE VIEW active_technician_worklogs AS SELECT * FROM technician_worklogs WHERE deleted_at IS NULL`,
	}
	for _, statement := range statements {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup tech efficiency database: %v", err)
		}
	}
}

func execTechFixture(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec failed: %v (q=%s)", err, q)
	}
}

// 測試一位技師多張工單、同一工單兩筆工時以 DISTINCT 計算工單數
func TestTechnicianEfficiency_DistinctWorkOrders(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	execTechFixture(t, `INSERT INTO users (id, username, name, role, active) VALUES (1, 'tech1', '張技師', 'technician', 1)`)
	execTechFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES
		(1, 'paid', '2026-01-05 10:00:00'),
		(2, 'paid', '2026-01-08 10:00:00')`)

	// 工單 1 有兩筆工時紀錄（同技師）：2 小時與 3 小時；工單 2 有一筆工時：4 小時
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(1, 1, '2026-01-05', 2.0, 1600, 320),
		(1, 1, '2026-01-06', 3.0, 2400, 480),
		(2, 1, '2026-01-08', 4.0, 3200, 640)`)

	html := technicianEfficiencyBlock("2026-01-01", "2026-01-31", true)

	// 技師姓名
	if !strings.Contains(html, "張技師") {
		t.Error("HTML 應包含張技師")
	}

	// 工單數應為 2（DISTINCT），而非 3
	if !strings.Contains(html, `<td class="num">2</td>`) {
		t.Error("工單數應為 2（DISTINCT work_order_id），不可因 3 筆工時算成 3")
	}

	// 總工時應為 9 (2 + 3 + 4)
	if !strings.Contains(html, `<td class="num">9</td>`) {
		t.Error("總工時應為 9 (2 + 3 + 4)")
	}
}

// 測試多人共同派工：同一工單在兩個技師身上各算一次工單數，但各自工時獨立
func TestTechnicianEfficiency_MultiTechSharedOrder(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	execTechFixture(t, `INSERT INTO users (id, username, name, role, active) VALUES
		(1, 'tech1', '張技師', 'technician', 1),
		(2, 'tech2', '李技師', 'technician', 1)`)
	execTechFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES
		(1, 'paid', '2026-01-05 10:00:00'),
		(2, 'paid', '2026-01-08 10:00:00')`)

	// 工單 1 由張技師（3 小時）與李技師（5 小時）共同施工
	// 工單 2 僅張技師施工（2 小時）
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(1, 1, '2026-01-05', 3.0, 2400, 480),
		(1, 2, '2026-01-05', 5.0, 4000, 800),
		(2, 1, '2026-01-08', 2.0, 1600, 320)`)

	html := technicianEfficiencyBlock("2026-01-01", "2026-01-31", false)

	// 張技師有 2 張工單 (工單1 + 工單2)，總工時 5
	// 李技師有 1 張工單 (工單1)，總工時 5
	if !strings.Contains(html, "張技師") || !strings.Contains(html, "李技師") {
		t.Error("HTML 應包含張技師與李技師")
	}
	if !strings.Contains(html, `<td class="num">2</td>`) {
		t.Error("張技師工單數應為 2")
	}
	if !strings.Contains(html, `<td class="num">1</td>`) {
		t.Error("李技師工單數應為 1")
	}
}

// 測試每工時計價與每工時計毛利之計算值正確，以及佣金為 0 正常顯示 0
func TestTechnicianEfficiency_PricingAndMargin(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	execTechFixture(t, `INSERT INTO users (id, username, name, role, active) VALUES
		(1, 'tech1', '張技師', 'technician', 1),
		(2, 'tech2', '王技師', 'technician', 1)`)
	execTechFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES
		(1, 'paid', '2026-01-05 10:00:00'),
		(2, 'paid', '2026-01-08 10:00:00')`)

	// 張技師：工時 4 小時，工資收入 4000，佣金 1200
	//   每工時計價 = 4000 / 4 = 1000
	//   佣金 = 1200
	//   每工時計毛利 = (4000 - 1200) / 4 = 700
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(1, 1, '2026-01-05', 4.0, 4000, 1200)`)

	// 王技師：工時 2 小時，工資收入 2000，佣金 0（測試佣金為 0 時顯示 0 而非未記錄）
	//   每工時計價 = 2000 / 2 = 1000
	//   佣金 = 0
	//   每工時計毛利 = (2000 - 0) / 2 = 1000
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(2, 2, '2026-01-08', 2.0, 2000, 0)`)

	html := technicianEfficiencyBlock("2026-01-01", "2026-01-31", true)

	// 檢查張技師的計價 1000、佣金 1200、毛利 700
	if !strings.Contains(html, `<td class="num">1000</td>`) {
		t.Error("應包含每工時計價 1000")
	}
	if !strings.Contains(html, `<td class="num">1200</td>`) {
		t.Error("應包含佣金 1200")
	}
	if !strings.Contains(html, `<td class="num">700</td>`) {
		t.Error("張技師每工時計毛利應為 700 ((4000-1200)/4)")
	}

	// 檢查王技師的佣金為 0（非未記錄）
	if !strings.Contains(html, `<td class="num">0</td>`) {
		t.Error("王技師佣金為 0 時應顯示 0")
	}
}

// 測試 showCost=false 時不顯示毛利與佣金欄位
func TestTechnicianEfficiency_ShowCostFalse(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	execTechFixture(t, `INSERT INTO users (id, username, name, role, active) VALUES (1, 'tech1', '張技師', 'technician', 1)`)
	execTechFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES (1, 'paid', '2026-01-05 10:00:00')`)
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(1, 1, '2026-01-05', 4.0, 4000, 1200)`)

	html := technicianEfficiencyBlock("2026-01-01", "2026-01-31", false)

	// showCost=false 時不可包含「毛利」或「佣金」欄位
	if strings.Contains(html, "毛利") {
		t.Error("showCost=false 時不應顯示毛利欄位")
	}
	if strings.Contains(html, "佣金") {
		t.Error("showCost=false 時不應顯示佣金欄位")
	}

	// 但仍須包含基礎效率指標
	for _, expectedHeader := range []string{"技師", "完成工單數", "工時總計", "每工時計價"} {
		if !strings.Contains(html, expectedHeader) {
			t.Errorf("showCost=false 時仍應顯示 %s 欄位", expectedHeader)
		}
	}
}

// 測試期間內已完成但無工時紀錄工單之備註數字正確
func TestTechnicianEfficiency_UnloggedCompletedOrders(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	execTechFixture(t, `INSERT INTO users (id, username, name, role, active) VALUES (1, 'tech1', '張技師', 'technician', 1)`)
	execTechFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES
		(1, 'paid', '2026-01-05 10:00:00'),        -- 有工時紀錄
		(2, 'completed', '2026-01-08 10:00:00'),   -- 無工時紀錄，期間內已完成（completed）
		(3, 'paid', '2026-01-12 10:00:00'),        -- 無工時紀錄，期間內已結帳（paid）
		(4, 'completed', '2026-01-15 10:00:00'),   -- 無工時紀錄，期間內已完成（completed）
		(5, 'pending', '2026-01-18 10:00:00'),     -- 無工時紀錄，但未完成（pending），不可計入
		(6, 'paid', '2026-02-05 10:00:00')         -- 無工時紀錄，但在期間外，不可計入`)

	// 只有工單 1 有工時
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(1, 1, '2026-01-05', 2.0, 1600, 320)`)

	html := technicianEfficiencyBlock("2026-01-01", "2026-01-31", false)

	expectedNote := "期間內已完成但無工時紀錄的工單 3 張"
	if !strings.Contains(html, expectedNote) {
		t.Errorf("備註應包含「%s」，實際 HTML 為: %s", expectedNote, html)
	}
}

// 測試跨期歸屬以 wl.work_date 為準
func TestTechnicianEfficiency_DateAttribution(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	execTechFixture(t, `INSERT INTO users (id, username, name, role, active) VALUES (1, 'tech1', '張技師', 'technician', 1)`)
	execTechFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES
		(1, 'paid', '2026-01-05 10:00:00')`)

	// 同一張工單，第一筆工時在 1 月、第二筆工時在 2 月
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(1, 1, '2026-01-15', 3.0, 3000, 600),
		(1, 1, '2026-02-05', 5.0, 5000, 1000)`)

	// 查 1 月
	htmlJan := technicianEfficiencyBlock("2026-01-01", "2026-01-31", false)
	if !strings.Contains(htmlJan, `<td class="num">3</td>`) {
		t.Error("1 月工時應為 3")
	}

	// 查 2 月
	htmlFeb := technicianEfficiencyBlock("2026-02-01", "2026-02-28", false)
	if !strings.Contains(htmlFeb, `<td class="num">5</td>`) {
		t.Error("2 月工時應為 5")
	}
}

// 測試工時為 0 時顯示「—」避免除以零
func TestTechnicianEfficiency_ZeroHoursShowsDash(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	execTechFixture(t, `INSERT INTO users (id, username, name, role, active) VALUES (1, 'tech1', '張技師', 'technician', 1)`)
	execTechFixture(t, `INSERT INTO work_orders (id, status, created_at) VALUES (1, 'paid', '2026-01-05 10:00:00')`)
	execTechFixture(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, labor_amount, commission_amount) VALUES
		(1, 1, '2026-01-05', 0, 0, 0)`)

	html := technicianEfficiencyBlock("2026-01-01", "2026-01-31", true)
	if !strings.Contains(html, `<td class="num">—</td>`) {
		t.Error("工時為 0 時每工時計價應顯示「—」")
	}
}

// 測試期間內無工時紀錄時顯示空狀態
func TestTechnicianEfficiency_Empty(t *testing.T) {
	setupTechEfficiencyTestDB(t)

	html := technicianEfficiencyBlock("2026-01-01", "2026-01-31", false)
	if !strings.Contains(html, "尚無技師工時紀錄") {
		t.Error("無紀錄時應顯示「尚無技師工時紀錄」")
	}
	if !strings.Contains(html, "期間內已完成但無工時紀錄的工單 0 張") {
		t.Error("無工時紀錄時仍應顯示缺口備註")
	}
}
