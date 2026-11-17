package service

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// 這組測試用「不開 commit 就直接關掉連線」模擬結帳途中被砍 process
// （WAL 模式下未認可的資料不能留下來）。改完結帳扣料後，這是最需要守的
// 不變量：庫存可以沒扣，但絕不能出現「扣了卻沒紀錄」或「有紀錄卻沒扣」。
//
// 兩個方向都測：
//   - 有 commit 的扣款一定要活過重開（否則測試本身可能是假的）
//   - 沒 commit 的扣款一定要整個消失

func openCrashDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crash.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("wal: %v", err)
	}
	for _, s := range []string{
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, name TEXT, stock_quantity INTEGER, reserved_quantity INTEGER)`,
		`CREATE TABLE inventory_movements (id INTEGER PRIMARY KEY, inventory_item_id INTEGER, delta INTEGER, before_quantity INTEGER, after_quantity INTEGER, reason TEXT, source TEXT, work_order_id INTEGER, created_by INTEGER)`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT, inventory_item_id INTEGER, quantity INTEGER)`,
		`INSERT INTO inventory_items (id,name,stock_quantity,reserved_quantity) VALUES (1,'煞車皮',10,0)`,
	} {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("setup: %v\n%s", err, s)
		}
	}
	return d, path
}

func stockOf(t *testing.T, d *sql.DB) (int, int) {
	t.Helper()
	var stock, reserved int
	if err := d.QueryRow(`SELECT stock_quantity, reserved_quantity FROM inventory_items WHERE id=1`).Scan(&stock, &reserved); err != nil {
		t.Fatalf("query stock: %v", err)
	}
	return stock, reserved
}

// TestCheckoutCommitPersists 是正控制項：_COMMIT_ 後重開，扣款與異動都要在。
// 沒有這條，「rollback 測試全綠」可能只是因為資料庫根本沒寫入。
func TestCheckoutCommitPersists(t *testing.T) {
	ctx := context.Background()
	d, path := openCrashDB(t)
	svc := NewInventoryService(d)

	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyWorkOrderCheckoutTx(ctx, tx, 7, 1, 3, 42); err != nil {
		tx.Rollback()
		t.Fatalf("deduct: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d2, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if stock, reserved := stockOf(t, d2); stock != 7 || reserved != 0 {
		t.Fatalf("COMMIT 後重開：stock=%d reserved=%d，應 7/0", stock, reserved)
	}
	var n int
	if err := d2.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("COMMIT 後重開：inventory_movements %d 筆，應 1 筆", n)
	}
}

// crashHelperProcess 在子測試行程裡跑：開庫、扣款、_不 commit_，然後用
// os.Exit(0) 直接終止——所有權仗由 OS 回收，等價於正式環境被砍 process。
func crashHelperProcess(path string) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		os.Exit(2)
	}
	if _, err := d.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		os.Exit(3)
	}
	for _, s := range []string{
		`CREATE TABLE inventory_items (id INTEGER PRIMARY KEY, name TEXT, stock_quantity INTEGER, reserved_quantity INTEGER)`,
		`CREATE TABLE inventory_movements (id INTEGER PRIMARY KEY, inventory_item_id INTEGER, delta INTEGER, before_quantity INTEGER, after_quantity INTEGER, reason TEXT, source TEXT, work_order_id INTEGER, created_by INTEGER)`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT, inventory_item_id INTEGER, quantity INTEGER)`,
		`INSERT INTO inventory_items (id,name,stock_quantity,reserved_quantity) VALUES (1,'煞車皮',10,0)`,
	} {
		if _, err := d.Exec(s); err != nil {
			os.Exit(4)
		}
	}
	tx, err := d.Begin()
	if err != nil {
		os.Exit(5)
	}
	if err := NewInventoryService(d).ApplyWorkOrderCheckoutTx(context.Background(), tx, 7, 1, 3, 42); err != nil {
		os.Exit(6)
	}
	// 關鍵：這裡什麼都不做就退出。不可加 defer close／不可 Rollback——那會變成
	// 「優雅的關閉」，而不是當掉的程序；正式環境的強殺不會跑任何清理。
	os.Exit(0)
}

// TestCheckoutCrashRollsBack 讓子行程扣了料卻「突然消失」，重開後庫存必須回到
// 10/0、異動必須 0 筆。Windows 上同一條未_commit 的交易無法靠關連線清乾淨
// （連線還被 tx 握著），所以真的生一支子行程用 os.Exit 模擬強殺。
func TestCheckoutCrashRollsBack(t *testing.T) {
	if os.Getenv("AR_CRASH_HELPER") == "1" {
		crashHelperProcess(os.Getenv("AR_CRASH_DB"))
		return
	}

	path := filepath.Join(t.TempDir(), "crash.db")
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCheckoutCrashRollsBack$")
	cmd.Env = append(os.Environ(), "AR_CRASH_HELPER=1", "AR_CRASH_DB="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("子行程（模擬強殺）失敗：%v\n%s", err, out)
	}

	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if stock, reserved := stockOf(t, d); stock != 10 || reserved != 0 {
		t.Fatalf("未 COMMIT 就斷線，扣款竟留下來了：stock=%d reserved=%d，應 10/0", stock, reserved)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("未 COMMIT 就斷線，異動竟留下來了：%d 筆", n)
	}
}
