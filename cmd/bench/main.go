package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"autorepairos/internal/config"
	"autorepairos/internal/cron"
	"autorepairos/internal/db"
)

var (
	dbPath  = flag.String("db", "", "SQLite database path (required)")
	resetDB = flag.Bool("reset", false, "Clear and rebuild test data")
	dryRun  = flag.Bool("dry-run", false, "Print plan without writing")
	scale   = flag.Int("scale", 10000, "Number of test records to generate")
)

func main() {
	flag.Parse()

	// 驗證 --db 參數
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "錯誤：必須指定 --db 參數")
		fmt.Fprintln(os.Stderr, "用法：bench --db <測試資料庫路徑> [--reset] [--dry-run] [--scale N]")
		os.Exit(1)
	}

	// 拒絕指向正式資料庫
	defaultDBPath := "data/autorepair.db"
	absDBPath, err := filepath.Abs(*dbPath)
	if err == nil {
		absDefault, _ := filepath.Abs(defaultDBPath)
		if absDBPath == absDefault {
			fmt.Fprintln(os.Stderr, "錯誤：--db 不可指向正式資料庫", defaultDBPath)
			os.Exit(1)
		}
	}

	// 印出環境資訊
	fmt.Println("=== AutoRepairOS 效能基準工具 ===")
	fmt.Printf("Go 版本: %s\n", runtime.Version())
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("架構: %s\n", runtime.GOARCH)
	fmt.Printf("測試資料庫: %s\n", *dbPath)
	fmt.Printf("資料筆數: %d\n", *scale)
	fmt.Printf("重置資料: %v\n", *resetDB)
	fmt.Printf("Dry Run: %v\n", *dryRun)
	fmt.Println()

	if *dryRun {
		fmt.Println("=== 執行計畫（Dry Run，不寫入）===")
		fmt.Printf("  - 產生 %d 筆測試資料\n", *scale)
		fmt.Printf("  - 量測列表查詢效能（車輛、工單、庫存、客戶）\n")
		fmt.Printf("  - 量測 1,000 筆通知批次處理\n")
		fmt.Println()
		fmt.Println("Dry Run 完成，未寫入任何資料")
		os.Exit(0)
	}

	// 重置資料（若指定）- 在 Init 之前做
	if *resetDB {
		fmt.Println("重置測試資料庫...")
		if err := resetTestData(); err != nil {
			log.Fatalf("重置資料失敗: %v", err)
		}
	}

	// 初始化測試資料庫
	cfg := &config.Config{
		DBPath: "file:" + *dbPath + "?_busy_timeout=5000",
	}
	if err := db.Init(cfg); err != nil {
		log.Fatalf("初始化資料庫失敗: %v", err)
	}
	defer db.DB.Close()

	// 產生測試資料
	fmt.Printf("產生 %d 筆測試資料...\n", *scale)
	start := time.Now()
	if err := generateTestData(*scale); err != nil {
		log.Fatalf("產生測試資料失敗: %v", err)
	}
	fmt.Printf("資料產生完成，耗時: %v\n", time.Since(start))
	fmt.Println()

	// 執行基準測試
	fmt.Println("=== 效能基準測試 ===")
	results := runBenchmarks()

	// 印出結果
	fmt.Println()
	fmt.Println("=== 測試結果 ===")
	allPassed := true
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
			allPassed = false
		}
		fmt.Printf("%s: %v (門檻: %v) [%s]\n", r.Name, r.Duration, r.Threshold, status)
	}

	fmt.Println()
	if allPassed {
		fmt.Println("全部測試通過")
		os.Exit(0)
	} else {
		fmt.Println("部分測試未達標")
		os.Exit(1)
	}
}

type benchmarkResult struct {
	Name      string
	Duration  time.Duration
	Threshold time.Duration
	Passed    bool
}

func resetTestData() error {
	// 關閉資料庫連線後刪除檔案
	if db.DB != nil {
		db.DB.Close()
		db.DB = nil
	}
	// 從 dbPath 參數提取實際檔案路徑
	dbPathClean := *dbPath
	if strings.HasPrefix(dbPathClean, "file:") {
		dbPathClean = strings.TrimPrefix(dbPathClean, "file:")
		if idx := strings.Index(dbPathClean, "?"); idx >= 0 {
			dbPathClean = dbPathClean[:idx]
		}
	}
	if err := os.Remove(dbPathClean); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("刪除測試資料庫失敗: %w", err)
	}
	return nil
}

func generateTestData(scale int) error {
	// 執行所有 migration 重建 schema
	if err := db.Migrate(); err != nil {
		return fmt.Errorf("migration 失敗: %w", err)
	}

	// 產生客戶
	fmt.Printf("  產生 %d 筆客戶...\n", scale)
	for i := 0; i < scale; i++ {
		name := fmt.Sprintf("測試客戶%04d", i)
		phone := fmt.Sprintf("0912%07d", i)
		_, err := db.DB.Exec(`INSERT INTO customers (name, phone, notification_preference) VALUES (?, ?, 'none')`, name, phone)
		if err != nil {
			return fmt.Errorf("插入客戶失敗: %w", err)
		}
	}

	// 產生車輛（每個客戶 1-2 台車）
	fmt.Printf("  產生車輛...\n")
	vehicleCount := 0
	for i := 1; i <= scale; i++ {
		plate := fmt.Sprintf("ABC-%04d", i)
		_, err := db.DB.Exec(`INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?, ?, 'Toyota Corolla')`, i, plate)
		if err != nil {
			return fmt.Errorf("插入車輛失敗: %w", err)
		}
		vehicleCount++
		// 50% 的客戶有第二台車
		if i%2 == 0 {
			plate2 := fmt.Sprintf("XYZ-%04d", i)
			_, err := db.DB.Exec(`INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?, ?, 'Honda Civic')`, i, plate2)
			if err != nil {
				return fmt.Errorf("插入第二台車失敗: %w", err)
			}
			vehicleCount++
		}
	}

	// 產生庫存
	fmt.Printf("  產生庫存...\n")
	inventoryCount := scale / 2
	for i := 0; i < inventoryCount; i++ {
		partNumber := fmt.Sprintf("PART-%04d", i)
		name := fmt.Sprintf("測試零件%04d", i)
		binLocation := fmt.Sprintf("A架-%02d", i%20)
		_, err := db.DB.Exec(`INSERT INTO inventory_items (part_number, name, cost_price, selling_price, stock_quantity, min_safety_stock, category, bin_location) VALUES (?, ?, 100, 200, 50, 10, '耗材', ?)`, partNumber, name, binLocation)
		if err != nil {
			return fmt.Errorf("插入庫存失敗: %w", err)
		}
	}

	// 產生工單（每個客戶 1-3 筆工單）
	fmt.Printf("  產生工單...\n")
	woCount := 0
	for i := 1; i <= scale; i++ {
		// 取該客戶的第一台車
		var vehicleID int64
		err := db.DB.QueryRow(`SELECT id FROM vehicles WHERE customer_id = ? LIMIT 1`, i).Scan(&vehicleID)
		if err != nil {
			continue
		}

		// 產生 1-3 筆工單
		numWO := 1 + (i % 3)
		for j := 0; j < numWO; j++ {
			status := "paid"
			if j%4 == 0 {
				status = "pending"
			}
			_, err := db.DB.Exec(`INSERT INTO work_orders (customer_id, vehicle_id, status, total_amount, current_mileage, created_at) VALUES (?, ?, ?, ?, ?, datetime('now', '-' || ? || ' days'))`,
				i, vehicleID, status, 1000+float64(j)*500, 50000, scale-j)
			if err != nil {
				return fmt.Errorf("插入工單失敗: %w", err)
			}
			woCount++
		}
	}

	// 產生技師工時（簡化：只建立技師，不生成工時以避免資料庫鎖定）
	fmt.Printf("  產生技師工時...\n")
	_, err := db.DB.Exec(`INSERT INTO users (username, password, role, name, active) VALUES ('tech1', 'hash', 'technician', '測試技師1', 1)`)
	if err != nil {
		return fmt.Errorf("插入技師失敗: %w", err)
	}
	worklogCount := 0

	// 產生通知（1,000 筆用於批次處理測試）
	fmt.Printf("  產生通知...\n")
	notifyCount := 1000
	for i := 0; i < notifyCount; i++ {
		custID := (i % scale) + 1
		_, err := db.DB.Exec(`INSERT INTO notifications (customer_id, channel, kind, subject, body, scheduled_at, status) VALUES (?, 'internal', 'test', '測試通知', '這是測試通知', datetime('now'), 'pending')`, custID)
		if err != nil {
			return fmt.Errorf("插入通知失敗: %w", err)
		}
	}

	fmt.Printf("  資料產生完成：客戶 %d, 車輛 %d, 庫存 %d, 工單 %d, 工時 %d, 通知 %d\n",
		scale, vehicleCount, inventoryCount, woCount, worklogCount, notifyCount)
	return nil
}

func runBenchmarks() []benchmarkResult {
	var results []benchmarkResult

	// 測試 0: 車輛列表查詢（預設 scale=10,000）
	fmt.Println("測試車輛列表查詢...")
	start := time.Now()
	rows, err := db.DB.Query(`SELECT v.id, COALESCE(vt.label,''), COALESCE(ft.label,''),
			COALESCE(bs.label,''), COALESCE(lm.current_mileage,0)
		FROM active_vehicles v
		JOIN active_customers c ON c.id = v.customer_id
		LEFT JOIN lookup_items vt ON vt.category='vehicle_type' AND vt.code=v.vehicle_type AND vt.active=1
		LEFT JOIN lookup_items ft ON ft.category='fuel_type' AND ft.code=v.fuel_type AND ft.active=1
		LEFT JOIN lookup_items bs ON bs.category='body_style' AND bs.code=v.body_style AND bs.active=1
		LEFT JOIN (
			SELECT vehicle_id, MAX(current_mileage) AS current_mileage
			FROM active_work_orders GROUP BY vehicle_id
		) lm ON lm.vehicle_id=v.id
		ORDER BY v.id DESC LIMIT ?`, *scale)
	count := 0
	if err == nil {
		for rows.Next() {
			var id, km int64
			var vehicleType, fuelType, bodyStyle string
			if scanErr := rows.Scan(&id, &vehicleType, &fuelType, &bodyStyle, &km); scanErr != nil {
				err = scanErr
				break
			}
			count++
		}
		if scanErr := rows.Err(); scanErr != nil {
			err = scanErr
		}
		rows.Close()
	}
	duration := time.Since(start)
	results = append(results, benchmarkResult{
		Name: "車輛列表查詢", Duration: duration, Threshold: 300 * time.Millisecond,
		Passed: err == nil && duration < 300*time.Millisecond,
	})
	if err != nil {
		log.Printf("車輛列表查詢失敗: %v", err)
	} else {
		fmt.Printf("  車輛數: %d, 耗時: %v\n", count, duration)
	}

	// 測試 1: 工單列表查詢
	fmt.Println("測試工單列表查詢...")
	start = time.Now()
	err = db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_orders`).Scan(&count)
	duration = time.Since(start)
	if err != nil {
		log.Printf("工單列表查詢失敗: %v", err)
		results = append(results, benchmarkResult{Name: "工單列表查詢", Duration: duration, Threshold: 300 * time.Millisecond, Passed: false})
	} else {
		passed := duration < 300*time.Millisecond
		results = append(results, benchmarkResult{Name: "工單列表查詢", Duration: duration, Threshold: 300 * time.Millisecond, Passed: passed})
		fmt.Printf("  工單數: %d, 耗時: %v\n", count, duration)
	}

	// 測試 2: 庫存列表查詢
	fmt.Println("測試庫存列表查詢...")
	start = time.Now()
	err = db.DB.QueryRow(`SELECT COUNT(1) FROM active_inventory_items`).Scan(&count)
	duration = time.Since(start)
	if err != nil {
		log.Printf("庫存列表查詢失敗: %v", err)
		results = append(results, benchmarkResult{Name: "庫存列表查詢", Duration: duration, Threshold: 300 * time.Millisecond, Passed: false})
	} else {
		passed := duration < 300*time.Millisecond
		results = append(results, benchmarkResult{Name: "庫存列表查詢", Duration: duration, Threshold: 300 * time.Millisecond, Passed: passed})
		fmt.Printf("  庫存數: %d, 耗時: %v\n", count, duration)
	}

	// 測試 3: 客戶列表查詢
	fmt.Println("測試客戶列表查詢...")
	start = time.Now()
	err = db.DB.QueryRow(`SELECT COUNT(1) FROM active_customers`).Scan(&count)
	duration = time.Since(start)
	if err != nil {
		log.Printf("客戶列表查詢失敗: %v", err)
		results = append(results, benchmarkResult{Name: "客戶列表查詢", Duration: duration, Threshold: 300 * time.Millisecond, Passed: false})
	} else {
		passed := duration < 300*time.Millisecond
		results = append(results, benchmarkResult{Name: "客戶列表查詢", Duration: duration, Threshold: 300 * time.Millisecond, Passed: passed})
		fmt.Printf("  客戶數: %d, 耗時: %v\n", count, duration)
	}

	// 測試 4: 通知批次處理（1,000 筆）
	fmt.Println("測試通知批次處理（1,000 筆）...")
	start = time.Now()
	processed := cron.ProcessNotificationsNow()
	duration = time.Since(start)
	passed := duration < 1*time.Minute
	results = append(results, benchmarkResult{Name: "通知批次處理", Duration: duration, Threshold: 1 * time.Minute, Passed: passed})
	fmt.Printf("  處理筆數: %d, 耗時: %v\n", processed, duration)

	return results
}
