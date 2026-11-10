package service

import (
	"database/sql"
	"errors"
	"math"
	"testing"
)

// ---------- Estimates branches ----------

func TestCreateEstimateBranches(t *testing.T) {
	db := setupTestDB(t)
	svc := New(db)
	t.Run("validation", func(t *testing.T) {
		for _, in := range []EstimateInput{
			{VehicleID: 0, CreatedBy: 1},
			{VehicleID: 7, CreatedBy: 0},
			{VehicleID: 7, CreatedBy: 1, CurrentMileage: -1},
		} {
			if _, err := svc.CreateEstimate(bgCtx, in); err == nil {
				t.Fatal("expected error")
			}
		}
	})
	t.Run("vehicle not found", func(t *testing.T) {
		if _, err := svc.CreateEstimate(bgCtx, EstimateInput{VehicleID: 999, CreatedBy: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("customer mismatch", func(t *testing.T) {
		if _, err := svc.CreateEstimate(bgCtx, EstimateInput{VehicleID: 7, CustomerID: 2, CreatedBy: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("success with explicit customer and complaint trim", func(t *testing.T) {
		id, err := svc.CreateEstimate(bgCtx, EstimateInput{VehicleID: 7, CustomerID: 1, CurrentMileage: 100, CustomerComplaint: "  hi  ", CreatedBy: 3})
		if err != nil || id <= 0 {
			t.Fatalf("id=%d err=%v", id, err)
		}
	})
	t.Run("drop active_vehicles generic", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `DROP VIEW active_vehicles`)
		if _, err := New(db2).CreateEstimate(bgCtx, EstimateInput{VehicleID: 7, CreatedBy: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("insert trigger", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `CREATE TRIGGER rejE BEFORE INSERT ON estimates BEGIN SELECT RAISE(ABORT,'x'); END`)
		if _, err := New(db2).CreateEstimate(bgCtx, EstimateInput{VehicleID: 7, CreatedBy: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAddEstimateItemBranches(t *testing.T) {
	db := setupTestDB(t)
	svc := New(db)
	t.Run("validation table", func(t *testing.T) {
		for _, tc := range []struct {
			id int64
			in EstimateItemInput
		}{
			{0, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1}},
			{1, EstimateItemInput{ItemType: "bad", Description: "x", Quantity: 1}},
			{1, EstimateItemInput{ItemType: "labor", Description: "   ", Quantity: 1}},
			{1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 0}},
			{1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1, UnitPrice: -1}},
			{1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1, UnitPrice: math.NaN()}},
			{1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1, UnitPrice: math.Inf(1)}},
			{999, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1}},
		} {
			if err := svc.AddEstimateItem(bgCtx, tc.id, tc.in); err == nil {
				t.Fatalf("expected error for %+v", tc.in)
			}
		}
	})
	t.Run("not draft", func(t *testing.T) {
		mustExec(t, db, `UPDATE estimates SET status='converted' WHERE id=1`)
		if err := svc.AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1}); !errors.Is(err, ErrEstimateNotDraft) {
			t.Fatalf("%v", err)
		}
		mustExec(t, db, `UPDATE estimates SET status='draft' WHERE id=1`)
	})
	t.Run("insert trigger", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `CREATE TRIGGER rejEI BEFORE INSERT ON estimate_items BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db2).AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("recalculate trigger via update estimates", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `CREATE TRIGGER rejEU BEFORE UPDATE ON estimates BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db2).AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1}); err == nil {
			t.Fatal("expected recalc error")
		}
	})
	t.Run("success all types", func(t *testing.T) {
		db2 := setupTestDB(t)
		svc2 := New(db2)
		for _, typ := range []string{"labor", "part", "subcontract"} {
			if err := svc2.AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: typ, Description: typ, Quantity: 1, UnitPrice: 10, InventoryItemID: 3}); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestDeleteEstimateItemBranches(t *testing.T) {
	t.Run("not found estimate", func(t *testing.T) {
		if err := New(setupTestDB(t)).DeleteEstimateItem(bgCtx, 999, 1); !errors.Is(err, ErrEstimateNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("not draft", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `UPDATE estimates SET status='cancelled' WHERE id=1`)
		if err := New(db).DeleteEstimateItem(bgCtx, 1, 1); !errors.Is(err, ErrEstimateNotDraft) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("delete trigger", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `CREATE TRIGGER rejDel BEFORE DELETE ON estimate_items BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db).DeleteEstimateItem(bgCtx, 1, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("drop estimates generic requireDraft", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP TABLE estimates`)
		if err := New(db).DeleteEstimateItem(bgCtx, 1, 1); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSetEstimateDiscountBranches(t *testing.T) {
	db := setupTestDB(t)
	svc := New(db)
	t.Run("validation", func(t *testing.T) {
		for _, d := range []float64{-1, math.NaN(), math.Inf(-1)} {
			if err := svc.SetEstimateDiscount(bgCtx, 1, d); err == nil {
				t.Fatal("expected error")
			}
		}
	})
	t.Run("not found", func(t *testing.T) {
		if err := svc.SetEstimateDiscount(bgCtx, 999, 1); !errors.Is(err, ErrEstimateNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("exceeds subtotal", func(t *testing.T) {
		// seed estimate 1 has no items -> subtotal 0, any positive discount exceeds
		if err := svc.SetEstimateDiscount(bgCtx, 1, 10); err == nil {
			t.Fatal("expected exceeds error")
		}
	})
	t.Run("subtotal query error dropped items", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `DROP TABLE estimate_items`)
		if err := New(db2).SetEstimateDiscount(bgCtx, 1, 0); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("update trigger", func(t *testing.T) {
		db2 := setupTestDB(t)
		mustExec(t, db2, `CREATE TRIGGER rejSU BEFORE UPDATE ON estimates BEGIN SELECT RAISE(ABORT,'x'); END`)
		if err := New(db2).SetEstimateDiscount(bgCtx, 1, 0); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("success", func(t *testing.T) {
		db2 := setupTestDB(t)
		svc2 := New(db2)
		if err := svc2.AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 2, UnitPrice: 50}); err != nil {
			t.Fatal(err)
		}
		if err := svc2.SetEstimateDiscount(bgCtx, 1, 30); err != nil {
			t.Fatal(err)
		}
		var total, disc float64
		if err := db2.QueryRow(`SELECT total_amount,discount_amount FROM estimates WHERE id=1`).Scan(&total, &disc); err != nil || total != 70 || disc != 30 {
			t.Fatalf("total=%v disc=%v err=%v", total, disc, err)
		}
	})
}

func TestConvertEstimateBranches(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		if _, err := New(setupTestDB(t)).ConvertEstimate(bgCtx, 999); !errors.Is(err, ErrEstimateNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("not draft", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `UPDATE estimates SET status='cancelled' WHERE id=1`)
		if _, err := New(db).ConvertEstimate(bgCtx, 1); !errors.Is(err, ErrEstimateNotDraft) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("converted without valid id falls to not draft", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `UPDATE estimates SET status='converted', converted_work_order_id=NULL WHERE id=1`)
		if _, err := New(db).ConvertEstimate(bgCtx, 1); !errors.Is(err, ErrEstimateNotDraft) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("has no items", func(t *testing.T) {
		if _, err := New(setupTestDB(t)).ConvertEstimate(bgCtx, 1); !errors.Is(err, ErrEstimateHasNoItems) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("drop estimates generic", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP TABLE estimates`)
		if _, err := New(db).ConvertEstimate(bgCtx, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("count items error dropped items", func(t *testing.T) {
		db := setupTestDB(t)
		mustExec(t, db, `DROP TABLE estimate_items`)
		if _, err := New(db).ConvertEstimate(bgCtx, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("insert work order trigger", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1, UnitPrice: 10}); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `CREATE TRIGGER rejWOI BEFORE INSERT ON work_orders BEGIN SELECT RAISE(ABORT,'x'); END`)
		if _, err := svc.ConvertEstimate(bgCtx, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("copy items trigger", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1, UnitPrice: 10}); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `CREATE TRIGGER rejWOItem BEFORE INSERT ON work_order_items BEGIN SELECT RAISE(ABORT,'x'); END`)
		if _, err := svc.ConvertEstimate(bgCtx, 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("mark converted trigger", func(t *testing.T) {
		db := setupTestDB(t)
		svc := New(db)
		if err := svc.AddEstimateItem(bgCtx, 1, EstimateItemInput{ItemType: "labor", Description: "x", Quantity: 1, UnitPrice: 10}); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db, `CREATE TRIGGER rejConv BEFORE UPDATE ON estimates BEGIN SELECT RAISE(ABORT,'x'); END`)
		if _, err := svc.ConvertEstimate(bgCtx, 1); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRequireDraftAndRecalcDirect(t *testing.T) {
	db := setupTestDB(t)
	if err := requireDraftEstimate(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{errRow(db)}}, 1); err == nil {
		t.Fatal("expected generic error")
	}
	if err := requireDraftEstimate(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{noRowsRow(db)}}, 1); !errors.Is(err, ErrEstimateNotFound) {
		t.Fatalf("%v", err)
	}
	if err := requireDraftEstimate(bgCtx, &fakeExecutor{db: db, rowQueue: []*sql.Row{db.QueryRow(`SELECT 'converted'`)}}, 1); !errors.Is(err, ErrEstimateNotDraft) {
		t.Fatalf("%v", err)
	}
	if err := recalculateEstimate(bgCtx, &fakeExecutor{db: db, execs: []execOutcome{{err: errors.New("recalc boom")}}}, 1); err == nil {
		t.Fatal("expected recalc error")
	}
	if err := recalculateEstimate(bgCtx, db, 1); err != nil {
		t.Fatal(err)
	}
}
