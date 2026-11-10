package service

import (
	"database/sql"
	"errors"
	"testing"
)

func TestTransitionWorkOrderStatus_Legal(t *testing.T) {
	db := setupTestDB(t)
	mustExec(t, db, `INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount) VALUES (20, 1, 7, 'pending', 0)`)
	if err := New(db).TransitionWorkOrderStatus(bgCtx, 20, "in_progress", 1); err != nil {
		t.Fatalf("legal transition: %v", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=20`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "in_progress" {
		t.Fatalf("status=%q, want in_progress", status)
	}
}

func TestTransitionWorkOrderStatus_PaidImmutable(t *testing.T) {
	db := setupTestDB(t)
	mustExec(t, db, `INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount) VALUES (21, 1, 7, 'paid', 100)`)
	err := New(db).TransitionWorkOrderStatus(bgCtx, 21, "completed", 1)
	if !errors.Is(err, ErrAlreadyPaid) {
		t.Fatalf("err=%v, want ErrAlreadyPaid", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=21`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paid" {
		t.Fatalf("status mutated to %q", status)
	}
}

func TestTransitionWorkOrderStatus_NotFound(t *testing.T) {
	db := setupTestDB(t)
	for _, id := range []int64{9999, 0, -1} {
		err := New(db).TransitionWorkOrderStatus(bgCtx, id, "in_progress", 1)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("id=%d err=%v, want ErrNotFound", id, err)
		}
	}
}

func TestTransitionWorkOrderStatus_PaidIdempotent(t *testing.T) {
	db := setupTestDB(t)
	mustExec(t, db, `INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount) VALUES (22, 1, 7, 'paid', 100)`)
	if err := New(db).TransitionWorkOrderStatus(bgCtx, 22, "paid", 1); err != nil {
		t.Fatalf("paid idempotent: %v", err)
	}
}

func TestTransitionWorkOrderStatusTxFakeBranches(t *testing.T) {
	t.Run("exec error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db, execs: []execOutcome{{err: errors.New("upd boom")}}}
		if err := transitionWorkOrderStatusTx(bgCtx, fx, 10, "in_progress", 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("rows affected error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db, execs: []execOutcome{{res: fakeResult{affected: 0, err: errors.New("rows boom")}}}}
		if err := transitionWorkOrderStatusTx(bgCtx, fx, 10, "in_progress", 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("cas zero generic scan error", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{errRow(db)}}
		if err := transitionWorkOrderStatusTx(bgCtx, fx, 10, "in_progress", 1); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("cas zero paid target paid", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{db.QueryRow(`SELECT 'paid'`)}}
		if err := transitionWorkOrderStatusTx(bgCtx, fx, 10, "paid", 1); err != nil {
			t.Fatalf("paid idempotent via tx: %v", err)
		}
	})
	t.Run("cas zero non-paid status", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{db.QueryRow(`SELECT 'completed'`)}}
		if err := transitionWorkOrderStatusTx(bgCtx, fx, 10, "in_progress", 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("cas zero no rows", func(t *testing.T) {
		db := setupTestDB(t)
		fx := &fakeExecutor{db: db,
			execs:    []execOutcome{{res: fakeResult{affected: 0}}},
			rowQueue: []*sql.Row{noRowsRow(db)}}
		if err := transitionWorkOrderStatusTx(bgCtx, fx, 10, "in_progress", 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("nil service", func(t *testing.T) {
		var svc *Service
		if err := svc.TransitionWorkOrderStatus(bgCtx, 10, "in_progress", 1); err == nil {
			t.Fatal("expected nil-service error")
		}
		svc2 := New(nil)
		if err := svc2.TransitionWorkOrderStatus(bgCtx, 10, "in_progress", 1); err == nil {
			t.Fatal("expected nil-db error")
		}
	})
}
