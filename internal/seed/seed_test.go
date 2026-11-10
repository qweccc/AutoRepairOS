package seed

import (
	"database/sql"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"

	_ "modernc.org/sqlite"
)

func setupSeedDB(t *testing.T) func() {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password TEXT NOT NULL, role TEXT NOT NULL, name TEXT)`,
		`CREATE TABLE suppliers (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, phone TEXT, address TEXT, notes TEXT)`,
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY AUTOINCREMENT, supplier_id INTEGER, part_number TEXT NOT NULL UNIQUE, barcode TEXT, name TEXT NOT NULL, bin_location TEXT NOT NULL, cost_price REAL DEFAULT 0, selling_price REAL DEFAULT 0, stock_quantity INTEGER DEFAULT 0, min_safety_stock INTEGER DEFAULT 5)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	old := db.DB
	db.DB = d
	return func() {
		db.DB = old
		_ = d.Close()
	}
}

func testCfg() *config.Config {
	return &config.Config{
		DefaultOwner: "owner", DefaultOwnerPwd: "ownerpass",
		DefaultCounter: "counter", DefaultCounterPwd: "counterpass",
		DefaultTech: "tech", DefaultTechPwd: "techpass",
	}
}

func count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow("SELECT COUNT(1) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRunCreatesAll(t *testing.T) {
	defer setupSeedDB(t)()
	if err := Run(testCfg()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, "users"); n != 3 {
		t.Errorf("users = %d, want 3", n)
	}
	if n := count(t, "suppliers"); n != 4 {
		t.Errorf("suppliers = %d, want 4", n)
	}
	if n := count(t, "inventory_items"); n != 12 {
		t.Errorf("inventory_items = %d, want 12", n)
	}
	// bcrypt hash 應可驗證
	var hash string
	if err := db.DB.QueryRow(`SELECT password FROM users WHERE username='owner'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "ownerpass" || hash == "" {
		t.Error("密碼應被 bcrypt 雜湊")
	}
}

func TestRunIdempotent(t *testing.T) {
	defer setupSeedDB(t)()
	if err := Run(testCfg()); err != nil {
		t.Fatal(err)
	}
	if err := Run(testCfg()); err != nil {
		t.Fatal(err)
	}
	if n := count(t, "users"); n != 3 {
		t.Errorf("重跑後 users = %d, want 3", n)
	}
	if n := count(t, "suppliers"); n != 4 {
		t.Errorf("重跑後 suppliers = %d, want 4", n)
	}
	if n := count(t, "inventory_items"); n != 12 {
		t.Errorf("重跑後 inventory = %d, want 12", n)
	}
}

func TestRunOwnerNoPassword(t *testing.T) {
	defer setupSeedDB(t)()
	cfg := testCfg()
	cfg.DefaultOwnerPwd = ""
	if err := Run(cfg); err == nil {
		t.Fatal("owner 無密碼應回錯誤")
	}
}

func TestRunSkipsEmptyNonOwnerPasswords(t *testing.T) {
	defer setupSeedDB(t)()
	cfg := testCfg()
	cfg.DefaultCounterPwd = ""
	cfg.DefaultTechPwd = ""
	if err := Run(cfg); err != nil {
		t.Fatal(err)
	}
	if n := count(t, "users"); n != 1 {
		t.Errorf("只有 owner 應建立, users = %d", n)
	}
}

func TestRunQueryError(t *testing.T) {
	defer setupSeedDB(t)()
	db.DB.Close()
	if err := Run(testCfg()); err == nil {
		t.Fatal("DB 關閉時查詢應回錯誤")
	}
}

func TestRunSuppliersQueryError(t *testing.T) {
	defer setupSeedDB(t)()
	db.DB.Close()
	if err := RunSuppliers(testCfg()); err == nil {
		t.Fatal("應回錯誤")
	}
}

func TestRunInventoryQueryError(t *testing.T) {
	defer setupSeedDB(t)()
	db.DB.Close()
	if err := RunInventorySeed(testCfg()); err == nil {
		t.Fatal("應回錯誤")
	}
}

func TestRunSuppliersInsertError(t *testing.T) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	// suppliers 缺 notes 欄 → INSERT 失敗；但 COUNT(id) 查詢正常
	_, _ = d.Exec(`CREATE TABLE suppliers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, phone TEXT, address TEXT)`)
	old := db.DB
	db.DB = d
	defer func() { db.DB = old; _ = d.Close() }()
	if err := RunSuppliers(testCfg()); err == nil {
		t.Fatal("INSERT 失敗應回錯誤")
	}
}

func TestRunInventoryInsertError(t *testing.T) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	// inventory_items 缺 bin_location 欄 → INSERT 失敗
	_, _ = d.Exec(`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, supplier_id INTEGER, part_number TEXT, barcode TEXT, name TEXT, cost_price REAL, selling_price REAL, stock_quantity INTEGER, min_safety_stock INTEGER)`)
	old := db.DB
	db.DB = d
	defer func() { db.DB = old; _ = d.Close() }()
	if err := RunInventorySeed(testCfg()); err == nil {
		t.Fatal("INSERT 失敗應回錯誤")
	}
}

func TestRunSuppliersFailureWrapped(t *testing.T) {
	// users 正常、suppliers INSERT 失敗 → Run 應回「供應商種子失敗」
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	_, _ = d.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password TEXT NOT NULL, role TEXT NOT NULL, name TEXT)`)
	_, _ = d.Exec(`CREATE TABLE suppliers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, phone TEXT, address TEXT)`) // 缺 notes
	_, _ = d.Exec(`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, supplier_id INTEGER, part_number TEXT, barcode TEXT, name TEXT, bin_location TEXT, cost_price REAL, selling_price REAL, stock_quantity INTEGER, min_safety_stock INTEGER)`)
	old := db.DB
	db.DB = d
	defer func() { db.DB = old; _ = d.Close() }()
	if err := Run(testCfg()); err == nil {
		t.Fatal("供應商種子失敗應回錯誤")
	}
}

func TestRunInventoryFailureWrapped(t *testing.T) {
	// users + suppliers 正常、inventory INSERT 失敗 → Run 應回「庫存種子失敗」
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	_, _ = d.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password TEXT NOT NULL, role TEXT NOT NULL, name TEXT)`)
	_, _ = d.Exec(`CREATE TABLE suppliers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, phone TEXT, address TEXT, notes TEXT)`)
	_, _ = d.Exec(`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, supplier_id INTEGER, part_number TEXT, barcode TEXT, name TEXT, cost_price REAL, selling_price REAL, stock_quantity INTEGER, min_safety_stock INTEGER)`) // 缺 bin_location
	old := db.DB
	db.DB = d
	defer func() { db.DB = old; _ = d.Close() }()
	if err := Run(testCfg()); err == nil {
		t.Fatal("庫存種子失敗應回錯誤")
	}
}

func TestRunUserInsertError(t *testing.T) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	// users 缺 name 欄 → INSERT 失敗
	_, _ = d.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password TEXT NOT NULL, role TEXT NOT NULL)`)
	_, _ = d.Exec(`CREATE TABLE suppliers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, phone TEXT, address TEXT, notes TEXT)`)
	_, _ = d.Exec(`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, supplier_id INTEGER, part_number TEXT, barcode TEXT, name TEXT, bin_location TEXT, cost_price REAL, selling_price REAL, stock_quantity INTEGER, min_safety_stock INTEGER)`)
	old := db.DB
	db.DB = d
	defer func() { db.DB = old; _ = d.Close() }()
	if err := Run(testCfg()); err == nil {
		t.Fatal("INSERT 失敗應回錯誤")
	}
}
