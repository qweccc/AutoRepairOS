package vision

import "testing"

// TestParseInvoiceDoc 涵蓋模型輸出的各種不規格寫法（含供應商/日期/民國年）。
func TestParseInvoiceDoc(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantItems  int
		wantSuppl  string
		wantDate   string
		checkItems func(t *testing.T, items []InvoiceLine)
	}{
		{
			name:      "完整單據",
			in:        `{"supplier_name":"鋒勝汽車零件","order_date":"2026-10-14","items":[{"description":"機油 5W-30","quantity":4,"unit_price":650,"part_number":"OIL-530"}]}`,
			wantItems: 1, wantSuppl: "鋒勝汽車零件", wantDate: "2026-10-14",
			checkItems: func(t *testing.T, items []InvoiceLine) {
				if items[0].Quantity != 4 || items[0].UnitPrice != 650 || items[0].PartNumber != "OIL-530" {
					t.Fatalf("got %+v", items[0])
				}
			},
		},
		{
			name:      "民國日期轉公元",
			in:        `{"supplier_name":"東陽車業","order_date":"115/10/14","items":[{"description":"煞車片","quantity":2,"unit_price":1200}]}`,
			wantItems: 1, wantSuppl: "東陽車業", wantDate: "2026-10-14",
		},
		{
			name:      "斜線公元日期",
			in:        `{"supplier_name":"X","order_date":"2026/1/5","items":[]}`,
			wantItems: 0, wantSuppl: "X", wantDate: "2026-01-05",
		},
		{
			name:      "包圍欄加前言",
			in:        "辨識結果：\n```json\n{\"supplier_name\":\"三陽三宏\",\"order_date\":\"2026-10-01\",\"items\":[{\"description\":\"機油濾芯\",\"quantity\":10,\"unit_price\":120}]}\n```",
			wantItems: 1, wantSuppl: "三陽三宏", wantDate: "2026-10-01",
		},
		{
			name:      "數字給成字串（千分位 NT$）",
			in:        `{"supplier_name":"Y","items":[{"description":"水箱","quantity":"3","unit_price":"NT$1,250"}]}`,
			wantItems: 1, wantSuppl: "Y", wantDate: "",
			checkItems: func(t *testing.T, items []InvoiceLine) {
				if items[0].Quantity != 3 || items[0].UnitPrice != 1250 {
					t.Fatalf("字串數字解析失敗：%+v", items[0])
				}
			},
		},
		{
			name:      "數量缺漏或 0 → 正規化成 1",
			in:        `{"items":[{"description":"火星塞","unit_price":300},{"description":"濾芯","quantity":0,"unit_price":200}]}`,
			wantItems: 2,
			checkItems: func(t *testing.T, items []InvoiceLine) {
				for _, it := range items {
					if it.Quantity != 1 {
						t.Fatalf("數量應正規化為 1：%+v", it)
					}
				}
			},
		},
		{
			name:      "沒品名的列丟掉",
			in:        `{"items":[{"description":"","quantity":1,"unit_price":100},{"description":"皮帶","quantity":1,"unit_price":800}]}`,
			wantItems: 1,
			checkItems: func(t *testing.T, items []InvoiceLine) {
				if items[0].Description != "皮帶" {
					t.Fatalf("got %+v", items[0])
				}
			},
		},
		{name: "完全不是 JSON → 空文件", in: "我看不懂", wantItems: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseInvoiceDoc(tc.in)
			if doc == nil {
				t.Fatal("parseInvoiceDoc 不應回 nil")
			}
			if len(doc.Items) != tc.wantItems {
				t.Fatalf("items len = %d, want %d", len(doc.Items), tc.wantItems)
			}
			if tc.wantSuppl != "" && doc.SupplierName != tc.wantSuppl {
				t.Errorf("supplier = %q, want %q", doc.SupplierName, tc.wantSuppl)
			}
			if tc.wantDate != "" && doc.OrderDate != tc.wantDate {
				t.Errorf("date = %q, want %q", doc.OrderDate, tc.wantDate)
			}
			if tc.checkItems != nil && len(doc.Items) > 0 {
				tc.checkItems(t, doc.Items)
			}
		})
	}
}

// TestGuessCategory 驗證關鍵字猜類別：命中、長關鍵字優先、猜不到回空。
func TestGuessCategory(t *testing.T) {
	cases := []struct{ desc, want string }{
		{"機油 5W-30", "引擎"},
		{"機油濾芯", "引擎"},
		{"煞車皮", ""}, // 煞車皮是煞車系統，目前沒有這個類別 → 不硬猜
		{"火星塞 4 支裝", "引擎"},
		{"輪胎 205/55R16", "輪胎"},
		{"電瓶 70Ah", "電機音響"},
		{"變速箱油", "變速箱"},
		{"保險桿", "車身鈑金"},
		{"避震器", "傳動轉向"},
		{"水箱膠皮", "引擎"},
		{"", ""},
		{"完全不相關的東西", ""},
	}
	for _, c := range cases {
		if got := GuessCategory(c.desc); got != c.want {
			t.Errorf("GuessCategory(%q) = %q, want %q", c.desc, got, c.want)
		}
	}
}

// TestParseInvoiceDocGuessCategory 整條鏈：單據 JSON → 品項帶出猜到的類別。
func TestParseInvoiceDocGuessCategory(t *testing.T) {
	in := `{"supplier_name":"鋒勝","order_date":"2026-10-14","items":[
		{"description":"機油 5W-30","quantity":4,"unit_price":650},
		{"description":"輪胎 195/65R15","quantity":4,"unit_price":3200},
		{"description":"墊片","quantity":10,"unit_price":20}
	]}`
	doc := parseInvoiceDoc(in)
	if len(doc.Items) != 3 {
		t.Fatalf("items = %d", len(doc.Items))
	}
	if doc.Items[0].GuessCategory != "引擎" {
		t.Errorf("機油應猜引擎，got %q", doc.Items[0].GuessCategory)
	}
	if doc.Items[1].GuessCategory != "輪胎" {
		t.Errorf("輪胎應猜輪胎，got %q", doc.Items[1].GuessCategory)
	}
	if doc.Items[2].GuessCategory != "" {
		t.Errorf("墊片不該猜出類別，got %q", doc.Items[2].GuessCategory)
	}
}

// TestNormalizeDate 單獨測日期正規化（含民國年轉公元）。
func TestNormalizeDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-10-14", "2026-10-14"},
		{"2026/10/14", "2026-10-14"},
		{"115/10/14", "2026-10-14"},   // 民國 → 公元
		{"115.10.14", "2026-10-14"},
		{"2026/1/5", "2026-01-05"},
		{"", ""},
		{"garbage", "garbage"},       // 解析不了回原值
		{"2026-13-40", "2026-13-40"}, // 月份/日不合理也回原值
	}
	for _, c := range cases {
		if got := normalizeDate(c.in); got != c.want {
			t.Errorf("normalizeDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestJsonNumber 單獨測寬容數字解析。
func TestJsonNumber(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{`650`, 650},
		{`650.5`, 650.5},
		{`"650"`, 650},
		{`"1,250"`, 1250},
		{`"NT$1,250"`, 1250},
		{`"$99"`, 99},
		{`null`, 0},
		{`"abc"`, 0},
		{``, 0},
	}
	for _, c := range cases {
		if got := jsonNumber([]byte(c.in)); got != c.want {
			t.Errorf("jsonNumber(%s) = %v, want %v", c.in, got, c.want)
		}
	}
}
