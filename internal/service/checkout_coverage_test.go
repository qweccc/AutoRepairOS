package service

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// ---------- nil guards (all public methods) ----------

func TestNilGuardsAllMethods(t *testing.T) {
	var nilSvc *Service
	empty := &Service{}
	for _, tc := range []struct {
		name string
		run  func(s *Service) error
	}{
		{"Checkout", func(s *Service) error { return s.Checkout(bgCtx, 1, 1) }},
		{"CreateReceipt", func(s *Service) error {
			return s.CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "V", Cash: 1})
		}},
		{"AssociateReceipt", func(s *Service) error {
			return s.AssociateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "V", Cash: 1})
		}},
		{"UpdateReceipt", func(s *Service) error { return s.UpdateReceipt(bgCtx, 1, ReceiptInput{VoucherNo: "V"}) }},
		{"VoidReceipt", func(s *Service) error { return s.VoidReceipt(bgCtx, 1) }},
		{"CreateEstimate", func(s *Service) error {
			_, err := s.CreateEstimate(bgCtx, EstimateInput{VehicleID: 1, CreatedBy: 1})
			return err
		}},
		{"AddEstimateItem", func(s *Service) error {
			return s.AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1})
		}},
		{"DeleteEstimateItem", func(s *Service) error { return s.DeleteEstimateItem(bgCtx, 1, 1) }},
		{"SetEstimateDiscount", func(s *Service) error { return s.SetEstimateDiscount(bgCtx, 1, 1) }},
		{"ConvertEstimate", func(s *Service) error { _, err := s.ConvertEstimate(bgCtx, 1); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(nilSvc); err == nil {
				t.Fatal("nil service should error")
			}
			if err := tc.run(empty); err == nil {
				t.Fatal("nil db should error")
			}
		})
	}
}

// ---------- CreateReceipt validation (needs non-nil DB) ----------

func TestCreateReceiptValidationBranches(t *testing.T) {
	svc := New(setupTestDB(t))
	cases := []struct {
		name string
		in   ReceiptInput
	}{
		{"customer zero", ReceiptInput{VoucherNo: "V"}},
		{"voucher empty", ReceiptInput{CustomerID: 1}},
		{"negative cash", ReceiptInput{CustomerID: 1, VoucherNo: "V1", Cash: -1, PayTotal: 1}},
		{"negative card", ReceiptInput{CustomerID: 1, VoucherNo: "V2", Card: -1, PayTotal: 1}},
		{"negative check", ReceiptInput{CustomerID: 1, VoucherNo: "V3", CheckAmount: -1, PayTotal: 1}},
		{"negative transfer", ReceiptInput{CustomerID: 1, VoucherNo: "V4", TransferAmount: -1, PayTotal: 1}},
		{"negative paytotal", ReceiptInput{CustomerID: 1, VoucherNo: "V5", Cash: 1, PayTotal: -1}},
		{"zero total", ReceiptInput{CustomerID: 1, VoucherNo: "V6"}},
		{"mismatch high", ReceiptInput{CustomerID: 1, VoucherNo: "V7", Cash: 10, PayTotal: 20}},
		{"mismatch low", ReceiptInput{CustomerID: 1, VoucherNo: "V8", Cash: 10, PayTotal: 5}},
		{"negative workorder", ReceiptInput{CustomerID: 1, VoucherNo: "V9", Cash: 1, PayTotal: 1, WorkOrderID: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := svc.CreateReceipt(bgCtx, c.in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCreateReceiptDBBranches(t *testing.T) {
	t.Run("work order not found", func(t *testing.T) {
		svc := New(setupTestDB(t))
		err := svc.CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "NF1", WorkOrderID: 999, Cash: 10, PayTotal: 10})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("want ErrNotFound got %v", err)
		}
	})
	t.Run("customer mismatch", func(t *testing.T) {
		svc := New(setupTestDB(t))
		err := svc.CreateReceipt(bgCtx, ReceiptInput{CustomerID: 2, VoucherNo: "MM1", WorkOrderID: 10, Cash: 10, PayTotal: 10})
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("want mismatch got %v", err)
		}
	})
	t.Run("standalone customer not found", func(t *testing.T) {
		svc := New(setupTestDB(t))
		if err := svc.CreateReceipt(bgCtx, ReceiptInput{CustomerID: 999, VoucherNo: "NF2", Cash: 10, PayTotal: 10}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("standalone success", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "OK1", PayDate: "2026-01-01", Cash: 10, PayTotal: 10, CreatedBy: 5}); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM receivables WHERE voucher_no='OK1' AND vehicle_id IS NULL AND work_order_id IS NULL`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
	t.Run("drop receivables generic error", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP TABLE receivables`)
		if err := New(db).CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "E1", Cash: 1, PayTotal: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("drop active_work_orders generic error", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP VIEW active_work_orders`)
		if err := New(db).CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "E2", WorkOrderID: 10, Cash: 1, PayTotal: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("drop active_customers generic error", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP VIEW active_customers`)
		if err := New(db).CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "E3", Cash: 1, PayTotal: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("duplicate voucher different data", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		in := ReceiptInput{CustomerID: 1, VoucherNo: "DUP", Cash: 10, PayTotal: 10}
		if err := svc.CreateReceipt(bgCtx, in); err != nil {
			t.Fatal(err)
		}
		in.Cash, in.PayTotal = 20, 20
		if err := svc.CreateReceipt(bgCtx, in); !errors.Is(err, ErrDuplicateVoucher) {
			t.Fatalf("want duplicate got %v", err)
		}
		// identical replay is a no-op success
		if err := svc.CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "DUP", Cash: 10, PayTotal: 10}); err != nil {
			t.Fatalf("identical replay: %v", err)
		}
	})
	t.Run("insert trigger error", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `CREATE TRIGGER rej BEFORE INSERT ON receivables BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db).CreateReceipt(bgCtx, ReceiptInput{CustomerID: 1, VoucherNo: "E4", Cash: 1, PayTotal: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
}

// ---------- UpdateReceipt branches ----------

func TestUpdateReceiptBranches(t *testing.T) {
	db := setupTestDB(t)
	svc := New(db)
	// seed two standalone receipts
	mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,pay_total,cash,source) VALUES (20,'U-OLD',1,50,50,'manual'),(21,'U-OTHER',1,50,50,'manual')`)
	mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,pay_total,cash,source,voided) VALUES (22,'U-VOID',1,50,50,'manual',1)`)

	cases := []struct {
		name string
		id   int64
		in   ReceiptInput
		want error
	}{
		{"id zero", 0, ReceiptInput{VoucherNo: "X"}, ErrReceiptNotFound},
		{"id negative", -1, ReceiptInput{VoucherNo: "X"}, ErrReceiptNotFound},
		{"voucher empty", 20, ReceiptInput{}, nil},
		{"negative cash", 20, ReceiptInput{VoucherNo: "X", Cash: -1}, nil},
		{"negative card", 20, ReceiptInput{VoucherNo: "X", Card: -1}, nil},
		{"negative check", 20, ReceiptInput{VoucherNo: "X", CheckAmount: -1}, nil},
		{"negative transfer", 20, ReceiptInput{VoucherNo: "X", TransferAmount: -1}, nil},
		{"negative paytotal", 20, ReceiptInput{VoucherNo: "X", PayTotal: -1}, nil},
		{"not found", 999, ReceiptInput{VoucherNo: "X"}, ErrReceiptNotFound},
		{"voided", 22, ReceiptInput{VoucherNo: "X"}, ErrReceiptVoided},
		{"duplicate voucher", 20, ReceiptInput{VoucherNo: "U-OTHER"}, ErrDuplicateVoucher},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.UpdateReceipt(bgCtx, c.id, c.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("want %v got %v", c.want, err)
			}
		})
	}

	t.Run("success standalone", func(t *testing.T) {
		if err := svc.UpdateReceipt(bgCtx, 20, ReceiptInput{VoucherNo: "U-NEW", PayDate: "2026-02-02", PayTotal: 60, Cash: 60, Notes: "n"}); err != nil {
			t.Fatal(err)
		}
		var v string
		var amt float64
		if err := db.QueryRow(`SELECT voucher_no,pay_total FROM receivables WHERE id=20`).Scan(&v, &amt); err != nil || v != "U-NEW" || amt != 60 {
			t.Fatalf("v=%s amt=%v err=%v", v, amt, err)
		}
	})
	t.Run("drop receivables generic error", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `DROP TABLE receivables`)
		if err := New(db2).UpdateReceipt(bgCtx, 1, ReceiptInput{VoucherNo: "X"}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("update trigger error", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `INSERT INTO receivables (id,voucher_no,customer_id,pay_total,cash,source) VALUES (30,'T1',1,10,10,'manual')`)
		mustExec(t, db2, `CREATE TRIGGER rejU BEFORE UPDATE ON receivables BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db2).UpdateReceipt(bgCtx, 30, ReceiptInput{VoucherNo: "T2"}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("receiptCompletes error via dropped view", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total,cash,source) VALUES (31,'T3',1,10,10,10,'manual')`)
		mustExec(t, db2, `DROP VIEW active_work_orders`)
		if err := New(db2).UpdateReceipt(bgCtx, 31, ReceiptInput{VoucherNo: "T4", PayTotal: 125, Cash: 125}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("partial update no checkout", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total,cash,source) VALUES (32,'P1',1,10,10,10,'manual')`)
		if err := New(db2).UpdateReceipt(bgCtx, 32, ReceiptInput{VoucherNo: "P2", PayTotal: 20, Cash: 20}); err != nil {
			t.Fatal(err)
		}
		var st string
		if err := db2.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&st); err != nil || st != "completed" {
			t.Fatalf("status=%s err=%v", st, err)
		}
	})
}

// ---------- VoidReceipt branches ----------

func TestVoidReceiptBranches(t *testing.T) {
	t.Run("invalid ids and not found", func(t *testing.T) {
		svc := New(setupTestDB(t))
		for _, id := range []int64{0, -1, 999} {
			if err := svc.VoidReceipt(bgCtx, id); !errors.Is(err, ErrReceiptNotFound) {
				t.Fatalf("id %d: %v", id, err)
			}
		}
	})
	t.Run("already voided", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,pay_total,voided) VALUES (40,'V',1,10,1)`)
		if err := New(db).VoidReceipt(bgCtx, 40); !errors.Is(err, ErrReceiptVoided) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("drop receivables generic load error", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP TABLE receivables`)
		if err := New(db).VoidReceipt(bgCtx, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("standalone void success", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,pay_total) VALUES (41,'V2',1,10)`)
		if err := New(db).VoidReceipt(bgCtx, 41); err != nil {
			t.Fatal(err)
		}
		var v int
		if err := db.QueryRow(`SELECT voided FROM receivables WHERE id=41`).Scan(&v); err != nil || v != 1 {
			t.Fatalf("voided=%d err=%v", v, err)
		}
	})
	t.Run("update trigger error", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,pay_total) VALUES (42,'V3',1,10)`)
		mustExec(t, db, `CREATE TRIGGER rejV BEFORE UPDATE ON receivables BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db).VoidReceipt(bgCtx, 42); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("update ignore trigger affected zero", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,pay_total) VALUES (43,'V4',1,10)`)
		mustExec(t, db, `CREATE TRIGGER ignV BEFORE UPDATE ON receivables BEGIN SELECT RAISE(IGNORE); END`)
		if err := New(db).VoidReceipt(bgCtx, 43); !errors.Is(err, ErrReceiptVoided) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("work order missing in recheck returns nil", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (44,'V5',1,999,10)`)
		if err := New(db).VoidReceipt(bgCtx, 44); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("recheck generic error dropped view", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (45,'V6',1,10,125)`)
		mustExec(t, db, `DROP VIEW active_work_orders`)
		if err := New(db).VoidReceipt(bgCtx, 45); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("paid but still fully received no revert", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.Checkout(bgCtx, 10, 1); err != nil {
			t.Fatal(err)
		}
		// two receipts totalling 125; void one of 25 -> still 100 <125 would revert, so use small
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (46,'VA',1,10,125),(47,'VB',1,10,10)`)
		// checkout recalculated total from items: 50+75=125. Void VB (10): received=125 still covers.
		if err := svc.VoidReceipt(bgCtx, 47); err != nil {
			t.Fatal(err)
		}
		var st string
		if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&st); err != nil || st != "paid" {
			t.Fatalf("status=%s err=%v", st, err)
		}
	})
	t.Run("revert status trigger error", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.Checkout(bgCtx, 10, 1); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (48,'VC',1,10,125)`)
		mustExec(t, db, `CREATE TRIGGER rejWO BEFORE UPDATE ON work_orders BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := svc.VoidReceipt(bgCtx, 48); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("load parts error dropped table", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.Checkout(bgCtx, 10, 1); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (49,'VD',1,10,125)`)
		mustExec(t, db, `DROP TABLE work_order_items`)
		if err := svc.VoidReceipt(bgCtx, 49); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("scan part error text quantity", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.Checkout(bgCtx, 10, 1); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (50,'VE',1,10,125)`)
		// replace part row quantity with non-numeric text (sqlite flexible typing); drop+recreate to allow text
		mustExec(t, db, `DROP TABLE work_order_items`)
		mustExec(t, db, `CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT, inventory_item_id INTEGER, quantity INTEGER)`)
		mustExec(t, db, `INSERT INTO work_order_items VALUES (9,10,'part',3,'abc')`)
		if err := svc.VoidReceipt(bgCtx, 50); err == nil {
			t.Fatal("expected scan error")
		}
	})
	t.Run("reverse inventory item missing", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.Checkout(bgCtx, 10, 1); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (51,'VF',1,10,125)`)
		mustExec(t, db, `DELETE FROM inventory_items WHERE id=3`)
		if err := svc.VoidReceipt(bgCtx, 51); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("reverse inventory trigger error", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.Checkout(bgCtx, 10, 1); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (52,'VG',1,10,125)`)
		mustExec(t, db, `CREATE TRIGGER rejInv BEFORE UPDATE ON inventory_items BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := svc.VoidReceipt(bgCtx, 52); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("movement insert trigger error", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.Checkout(bgCtx, 10, 1); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `INSERT INTO receivables (id,voucher_no,customer_id,work_order_id,pay_total) VALUES (53,'VH',1,10,125)`)
		mustExec(t, db, `CREATE TRIGGER rejMov BEFORE INSERT ON inventory_movements BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := svc.VoidReceipt(bgCtx, 53); err == nil {
			t.Fatal("expected error")
		}
	})
}

// ---------- withTx branches ----------

func TestWithTxBranches(t *testing.T) {
	t.Run("closed db conn error", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		_ = db.Close()
		if err := svc.Checkout(bgCtx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("fn error rolls back", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		err := svc.withTx(bgCtx, "test", func(tx sqlExecutor) error {
			if _, e := tx.ExecContext(bgCtx, `UPDATE work_orders SET status='paid' WHERE id=10`); e != nil {
				return e
			}
			return errors.New("boom")
		})
		if err == nil {
			t.Fatal("expected error")
		}
		var st string
		if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=10`).Scan(&st); err != nil || st != "completed" {
			t.Fatalf("status=%s err=%v", st, err)
		}
	})
	t.Run("commit deferred FK error", func(t *testing.T) {
		name := strings.ReplaceAll(t.Name(), "/", "_") + "fk"
		db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		defer db.Close()
		mustExec(t, db, `PRAGMA foreign_keys=ON`)
		mustExec(t, db, `CREATE TABLE parent (id INTEGER PRIMARY KEY)`)
		mustExec(t, db, `CREATE TABLE child (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)`)
		svc := New(db)
		err = svc.withTx(bgCtx, "fk", func(tx sqlExecutor) error {
			_, e := tx.ExecContext(bgCtx, `INSERT INTO child (id,pid) VALUES (1,999)`)
			return e
		})
		if err == nil {
			t.Fatal("expected commit FK error")
		}
	})
}

// ---------- existingReceiptMatches / receiptCompletes / helpers (direct) ----------

func receiptRow(db *sql.DB, voucher string, cust int64, source string) *sql.Row {
	// columns: voucher,customer,wo,paydate,detail,real,discount,tax,paytotal,cash,card,check,transfer,ref,cardno,checkno,checkdate,bank,notes,source
	return db.QueryRow(`SELECT ?,?,?, '2026-01-01',0,0,0,0,10,10,0,0,0,'','','','','','',?`, voucher, cust, int64(0), source)
}

func TestExistingReceiptMatchesDirect(t *testing.T) {
	db := setupTestDB(t)
	in := ReceiptInput{VoucherNo: "X1", CustomerID: 1, PayDate: "2026-01-01", PayTotal: 10, Cash: 10}
	// generic error
	if _, err := existingReceiptMatches(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{errRow(db)}}, in); err == nil {
		t.Fatal("expected error")
	}
	// not found
	if ok, err := existingReceiptMatches(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{noRowsRow(db)}}, in); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// manual + identical -> matched
	if ok, err := existingReceiptMatches(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{receiptRow(db, "X1", 1, "manual")}}, in); !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	// manual + different customer -> duplicate
	if _, err := existingReceiptMatches(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{receiptRow(db, "X1", 2, "manual")}}, in); !errors.Is(err, ErrDuplicateVoucher) {
		t.Fatalf("%v", err)
	}
	// non-manual source + identical -> duplicate
	if _, err := existingReceiptMatches(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{receiptRow(db, "X1", 1, "auto")}}, in); !errors.Is(err, ErrDuplicateVoucher) {
		t.Fatalf("%v", err)
	}
}

func TestReceiptCompletesDirect(t *testing.T) {
	db := setupTestDB(t)
	// generic error
	if _, err := receiptCompletesWorkOrder(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{errRow(db)}}, 10); err == nil {
		t.Fatal("expected error")
	}
	// not found
	if _, err := receiptCompletesWorkOrder(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{noRowsRow(db)}}, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	// real DB branches: status != completed
	mustExec(t, db, `INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount) VALUES (11,1,7,'pending',100)`)
	if ok, err := receiptCompletesWorkOrder(bgCtx, db, 11); ok || err != nil {
		t.Fatalf("pending ok=%v err=%v", ok, err)
	}
	// total <= 0
	mustExec(t, db, `INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount) VALUES (12,1,7,'completed',0)`)
	if ok, err := receiptCompletesWorkOrder(bgCtx, db, 12); ok || err != nil {
		t.Fatalf("zero total ok=%v err=%v", ok, err)
	}
	// completed but not enough received
	if ok, err := receiptCompletesWorkOrder(bgCtx, db, 10); ok || err != nil {
		t.Fatalf("not enough ok=%v err=%v", ok, err)
	}
	// completed and enough: insert receipt 125
	mustExec(t, db, `INSERT INTO receivables (voucher_no,customer_id,work_order_id,pay_total) VALUES ('RC',1,10,125)`)
	if ok, err := receiptCompletesWorkOrder(bgCtx, db, 10); !ok || err != nil {
		t.Fatalf("enough ok=%v err=%v", ok, err)
	}
}

// ---------- checkoutTx direct fake branches ----------

func TestCheckoutTxFakeBranches(t *testing.T) {
	okRes := func() sql.Result { return fakeResult{affected: 1} }
	errRes := func() sql.Result { return fakeResult{affected: 0, err: errors.New("rows affected boom")} }

	t.Run("recalc fails both then already paid", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{err: errors.New("r1")}, {err: errors.New("r2")}, {res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{db.QueryRow(`SELECT 'paid'`)}}
		if err := checkoutTx(bgCtx, fx, 10, 1); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("affected zero status completed", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: okRes()}, {res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{db.QueryRow(`SELECT 'completed'`)}}
		if err := checkoutTx(bgCtx, fx, 10, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("affected zero status not found", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: okRes()}, {res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{noRowsRow(db)}}
		if err := checkoutTx(bgCtx, fx, 10, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("affected zero status generic error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: okRes()}, {res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{errRow(db)}}
		if err := checkoutTx(bgCtx, fx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("mark paid exec error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db, execs: []execOutcome{{res: okRes()}, {err: errors.New("upd boom")}}}
		if err := checkoutTx(bgCtx, fx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("rows affected error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db, execs: []execOutcome{{res: okRes()}, {res: errRes()}}}
		if err := checkoutTx(bgCtx, fx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("customer load not found", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: okRes()}, {res: okRes()}},
			rowQueue: []*sql.Row{noRowsRow(db)}}
		if err := checkoutTx(bgCtx, fx, 10, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("customer load generic error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: okRes()}, {res: okRes()}},
			rowQueue: []*sql.Row{errRow(db)}}
		if err := checkoutTx(bgCtx, fx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("load parts query error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: okRes()}, {res: okRes()}},
			rowQueue: []*sql.Row{db.QueryRow(`SELECT 1`)},
			queries:  []queryOutcome{{err: errors.New("parts boom")}}}
		if err := checkoutTx(bgCtx, fx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestCheckoutTxRealBranches(t *testing.T) {
	t.Run("insufficient stock real", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `UPDATE inventory_items SET stock_quantity=1 WHERE id=3`)
		if err := New(db).Checkout(bgCtx, 10, 1); err == nil {
			t.Fatal("expected insufficient inventory")
		}
	})
	t.Run("scan part text quantity real", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `UPDATE work_order_items SET quantity='abc' WHERE id=1`)
		if err := New(db).Checkout(bgCtx, 10, 1); err == nil {
			t.Fatal("expected scan error")
		}
	})
	t.Run("load parts dropped table", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP TABLE work_order_items`)
		if err := New(db).Checkout(bgCtx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("mark paid update trigger", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `CREATE TRIGGER rejWO BEFORE UPDATE ON work_orders BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db).Checkout(bgCtx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("deduct trigger generic", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `CREATE TRIGGER rejInv BEFORE UPDATE ON inventory_items BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db).Checkout(bgCtx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("movement trigger", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `CREATE TRIGGER rejMov BEFORE INSERT ON inventory_movements BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db).Checkout(bgCtx, 10, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("already paid idempotent", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		for i := 0; i < 2; i++ {
			if err := svc.Checkout(bgCtx, 10, 1); err != nil {
				t.Fatal(err)
			}
		}
	})
}
