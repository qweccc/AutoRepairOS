package seed

import (
	"fmt"
	"log"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
)

// Supplier 種子：建立預設供應商。已存在者以 INSERT OR IGNORE 略過，可重複安全呼叫。
func RunSuppliers(cfg *config.Config) error {
	defaults := []struct {
		id      int
		name    string
		phone   string
		address string
		notes   string
	}{
		{1, "鋒勝汽車零件", "02-2300-0001", "台北 仁愛路三段 88 號", "機油系、機油濾芯"},
		{2, "東陽車業總經銷", "02-2750-0002", "台北 松山 八德路四段", "煞車、懸吊"},
		{3, "三陽三宏機油", "02-2940-0003", "新北 三重 文化路", "機油、機油濾芯"},
		{4, "正廠冷氣配件行", "02-8790-0004", "台北 內湖 民權路", "冷氣、冷媒"},
	}

	for _, s := range defaults {
		var exists int
		err := db.DB.QueryRow("SELECT COUNT(1) FROM suppliers WHERE id = ?", s.id).Scan(&exists)
		if err != nil {
			return fmt.Errorf("查詢供應商 %d 失敗: %v", s.id, err)
		}
		if exists > 0 {
			log.Printf("供應商已存在，略過: %s", s.name)
			continue
		}

		_, err = db.DB.Exec(
			"INSERT INTO suppliers (id, name, phone, address, notes) VALUES (?, ?, ?, ?, ?)",
			s.id, s.name, s.phone, s.address, s.notes,
		)
		if err != nil {
			return fmt.Errorf("建立供應商 %s 失敗: %v", s.name, err)
		}
		log.Printf("建立供應商: %s", s.name)
	}

	return nil
}

// RunInventorySeed 建立預設零件庫存。已存在者以 INSERT OR IGNORE 略過，可重複安全呼叫。
func RunInventorySeed(cfg *config.Config) error {
	defaults := []struct {
		id       int
		partNo   string
		barcode  string
		supplier int
		name     string
		binLoc   string
		cost     float64
		sell     float64
		stock    int
		minSafe  int
	}{
		{1, "0W20", "BR0W20A001", 3, "機油 5W-30 (LNG)", "A-01-1", 480, 980, 30, 5},
		{2, "MF1", "BRMF1A0002", 1, "機油濾芯", "A-01-2", 220, 380, 40, 5},
		{3, "BP1", "BRBP1A0003", 1, "空氣濾芯", "A-02-1", 320, 550, 20, 3},
		{4, "SP1", "BRSP1A0004", 1, "火花塞 (4 支裝)", "A-03-1", 850, 1500, 12, 2},
		{5, "BBK", "BRBBKA0005", 2, "煞車片 (前)", "B-01-1", 980, 1800, 10, 2},
		{6, "CAL", "BRCALA0006", 4, "冷媒 R134a (1kg)", "C-01-1", 350, 620, 25, 5},
		{7, "BAT", "BRBATA0007", 2, "免保養電池 70Ah", "B-03-1", 2600, 3800, 6, 1},
		{8, "H2O", "BRH2OA0008", 1, "水箱膠皮", "B-05-1", 780, 1300, 10, 2},
		{9, "RAC", "BRRACA0009", 4, "冷氣壓縮機", "C-02-1", 4200, 6800, 4, 1},
		{10, "BLL", "BRBLLA0010", 2, "避震器 (前左)", "B-02-1", 1800, 3200, 8, 1},
		{11, "BLLR", "BRBLLR0011", 2, "避震器 (後左)", "B-02-2", 1800, 3200, 8, 1},
		{12, "BELT", "BRBTLA0012", 1, "皮帶 (引擎)", "A-04-1", 560, 980, 15, 2},
	}

	for _, it := range defaults {
		var exists int
		err := db.DB.QueryRow("SELECT COUNT(1) FROM inventory_items WHERE id = ?", it.id).Scan(&exists)
		if err != nil {
			return fmt.Errorf("查詢零件 %d 失敗: %v", it.id, err)
		}
		if exists > 0 {
			log.Printf("零件已存在，略過: %s", it.name)
			continue
		}

		_, err = db.DB.Exec(`INSERT INTO inventory_items
			(id, supplier_id, part_number, barcode, name, bin_location,
			 cost_price, selling_price, stock_quantity, min_safety_stock)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			it.id, it.supplier, it.partNo, it.barcode, it.name, it.binLoc,
			it.cost, it.sell, it.stock, it.minSafe)
		if err != nil {
			return fmt.Errorf("建立零件 %s 失敗: %v", it.name, err)
		}
		log.Printf("建立零件: %s (供應商#%d, 售價 %s, 庫存 %d)", it.name, it.supplier, fmt.Sprintf("%.2f", it.sell), it.stock)
	}

	return nil
}
