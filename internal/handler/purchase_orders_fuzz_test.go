package handler

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// FuzzParseItemsJSON 鎖住進貨單 items_json 解析的契約：
// 字串欄位必須去空白、數值必須是有限數、合法陣列的筆數不可遺失。
// 這個欄位是 OCR 開單與手動貼上好幾層字串拼接後才到 handler，最值得模糊測。
func FuzzParseItemsJSON(f *testing.F) {
	for _, s := range []string{
		`[]`,
		`[{"description":"機油","quantity":2,"unit_price":120,"part_number":"OIL-1","category":"lubrication"}]`,
		`[{"description":" 機油 ","quantity":-3,"unit_price":0}]`,
		`not json`,
		``,
		`{}`,
		`[1,2]`,
		`[{"description":"A","quantity":"2"}]`,
		`null`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		out := parseItemsJSON(raw)
		for i, it := range out {
			if it.Description != strings.TrimSpace(it.Description) ||
				it.PartNumber != strings.TrimSpace(it.PartNumber) ||
				it.Category != strings.TrimSpace(it.Category) {
				t.Fatalf("parseItemsJSON(%s)[%d] 欄位未去空白：%+v", raw, i, it)
			}
			if math.IsNaN(it.Quantity) || math.IsInf(it.Quantity, 0) ||
				math.IsNaN(it.UnitPrice) || math.IsInf(it.UnitPrice, 0) {
				t.Fatalf("parseItemsJSON(%s)[%d] 數量／單價不是有限數：%+v", raw, i, it)
			}
		}
		// 若同一個 raw 用相同 shape 能解析成功，回筆數必須一致——
		// 否則就是「JSON 合法但有些項目被悄悄丟掉」。
		var arr []struct {
			Description string  `json:"description"`
			Quantity    float64 `json:"quantity"`
			UnitPrice   float64 `json:"unit_price"`
			PartNumber  string  `json:"part_number"`
			Category    string  `json:"category"`
		}
		if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(out) != len(arr) {
			t.Fatalf("parseItemsJSON(%s) 回 %d 筆，但預期 %d 筆", raw, len(out), len(arr))
		}
	})
}
