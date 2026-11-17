package service

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// 這組測試不是「測某個函式對不對」，而是測「任何操作序列都不能把庫存推離不變量」。
//
// 具體做法：固定種子、重跑 N 次「亂數操作 → 驗不變量」迴圈，涵蓋三種會動到
// stock/reserved 的入口（一般調整 ApplyStockDelta、結帳扣料
// ApplyWorkOrderCheckoutTx、庫存扣減調整 ApplyStockAdjust）。只要有任一步讓
// 不變量破掉，測試就印出是第幾步、做了什麼、以及當時的列值。
//
// 與既有測試的分工：checkout_coverage / inventory_checkout 驗「單一情境的正確
// 答案」；這裡驗「任意順序下的整體性質」，後者才抓得到交互累積造成的漂移。
const propStepsPerSeed = 250

func TestInventoryInvariantsUnderRandomOperations(t *testing.T) {
	for _, seed := range []int64{1, 2, 3, 20261117} {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			testInventoryPropertyRun(t, seed)
		})
	}
}

func testInventoryPropertyRun(t *testing.T, seed int64) {
	db := setupInventorySvcDB(t)
	svc := NewInventoryService(db)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(seed))

	activeItems := []int64{1, 2} // 3 已軟刪除，不參與
	for step := 1; step <= propStepsPerSeed; step++ {
		itemID := activeItems[rng.Intn(len(activeItems))]
		switch op := rng.Intn(100); {
		case op < 45:
			// 一般調整：可正可負（入庫 / 手動扣料）
			delta := rng.Intn(21) - 10
			if delta == 0 {
				continue
			}
			_, _ = svc.ApplyStockDelta(ctx, int(itemID), delta, "property-test", "manual-adjust", "7")
			checkInventoryInvariants(t, db, step,
				fmt.Sprintf("ApplyStockDelta(item=%d, delta=%+d)", itemID, delta))
		case op < 75:
			// 結帳扣料：另外驗精確 oracle——成功的話
			//   stock' == stock-q、reserved' == max(reserved-q, 0)
			qty := rng.Intn(6) + 1
			beforeStock, beforeReserved := readStockPair(t, db, itemID)
			err := applyCheckoutInTx(t, ctx, db, svc, itemID, qty)
			if err != nil {
				continue // 庫存不足等拒絕是正常分支
			}
			afterStock, afterReserved := readStockPair(t, db, itemID)
			wantReserved := beforeReserved - qty
			if wantReserved < 0 {
				wantReserved = 0
			}
			if afterStock != beforeStock-qty || afterReserved != wantReserved {
				t.Fatalf("seed %d step %d 結帳 oracle 不符：item=%d qty=%d stock %d→%d（期望 %d）、reserved %d→%d（期望 %d）",
					seed, step, itemID, qty, beforeStock, afterStock, beforeStock-qty,
					beforeReserved, afterReserved, wantReserved)
			}
			checkInventoryInvariants(t, db, step,
				fmt.Sprintf("checkout(item=%d, qty=%d)", itemID, qty))
		default:
			// 庫存扣減調整（走 available = stock - reserved 檢查的那條路徑）
			qty := rng.Intn(8) + 1
			_ = svc.ApplyStockAdjust(ctx, int(itemID), qty, "property-test", "7")
			checkInventoryInvariants(t, db, step,
				fmt.Sprintf("ApplyStockAdjust(item=%d, qty=%d)", itemID, qty))
		}
	}

	assertMovementLedgerReconciles(t, db, map[int64]int{1: 10, 2: 5})
}

func applyCheckoutInTx(t *testing.T, ctx context.Context, db *sql.DB, svc *InventoryService, itemID int64, qty int) error {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := svc.ApplyWorkOrderCheckoutTx(ctx, tx, 1, itemID, qty, 7); err != nil {
		return err
	}
	return tx.Commit()
}

func readStockPair(t *testing.T, db *sql.DB, itemID int64) (int, int) {
	t.Helper()
	var stock, reserved int
	if err := db.QueryRow(`SELECT COALESCE(stock_quantity,0), COALESCE(reserved_quantity,0)
		FROM inventory_items WHERE id = ?`, itemID).Scan(&stock, &reserved); err != nil {
		t.Fatal(err)
	}
	return stock, reserved
}

// checkInventoryInvariants 是模型本體：每跑一步就確認全部「啟用中」項目仍满足
//
//	stock_quantity     >= 0
//	reserved_quantity  >= 0
//	reserved_quantity  <= stock_quantity
func checkInventoryInvariants(t *testing.T, db *sql.DB, step int, op string) {
	t.Helper()
	rows, err := db.Query(`SELECT id, COALESCE(stock_quantity,0), COALESCE(reserved_quantity,0)
		FROM inventory_items WHERE deleted_at IS NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var id, stock, reserved int
		if err := rows.Scan(&id, &stock, &reserved); err != nil {
			t.Fatal(err)
		}
		if stock < 0 {
			problems = append(problems, fmt.Sprintf("item %d stock=%d 為負", id, stock))
		}
		if reserved < 0 {
			problems = append(problems, fmt.Sprintf("item %d reserved=%d 為負", id, reserved))
		}
		if reserved > stock {
			problems = append(problems, fmt.Sprintf("item %d reserved=%d > stock=%d", id, reserved, stock))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Fatalf("步驟 %d 的 %s 之後不變量被破壞：%s", step, op, strings.Join(problems, "; "))
	}
}

// assertMovementLedgerReconciles 驗另一條跨操作性質：從最初的种子值起算，
// 每個項目「所有異動的總和」必須剛好等於目前庫存。若某條路径漏記或重複記
// 異動，這裡就會對不起來——而逐筆單元測試不一定會發現。
func assertMovementLedgerReconciles(t *testing.T, db *sql.DB, initial map[int64]int) {
	t.Helper()
	for itemID, start := range initial {
		var stock int
		var sum sql.NullInt64
		if err := db.QueryRow(`SELECT
			(SELECT COALESCE(stock_quantity,0) FROM inventory_items WHERE id = ?),
			(SELECT COALESCE(SUM(delta),0) FROM inventory_movements WHERE inventory_item_id = ?)`,
			itemID, itemID).Scan(&stock, &sum); err != nil {
			t.Fatal(err)
		}
		if int(sum.Int64) != stock-start {
			t.Errorf("item %d 異動帳不平衡：初始 %d、目前 %d、異動總和 %d（應為 %+d）",
				itemID, start, stock, sum.Int64, stock-start)
		}
	}
}
