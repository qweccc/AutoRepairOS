package handler

import (
	"encoding/json"
	"strconv"
	"strings"

	"autorepairos/internal/model"
)

// vehicleOptionsJSON 把車輛清單轉成給前端即時搜尋用的 JSON。
//
// 輸出格式：[{"v":"21","label":"ABC-1234 / Toyota Corolla / 王小明"}, ...]
// label 刻意用「/」分隔三段，讓前端可以逐段比對（車牌、車型、車主），
// 做到「打車牌就只比車牌」的加權搜尋。
//
// 回傳的是已做 HTML 轉義後的 JSON 字串，可安全放進 data-options="..." 屬性。
func vehicleOptionsJSON(list []model.Vehicle) string {
	type item struct {
		V     string `json:"v"`
		Label string `json:"label"`
	}

	items := make([]item, 0, len(list))
	for _, v := range list {
		parts := []string{v.PlateNumber, v.BrandModel, v.CustomerName}
		clean := make([]string, 0, 3)
		for _, p := range parts {
			clean = append(clean, htmlEsc(strings.TrimSpace(p)))
		}
		items = append(items, item{
			V:     strconv.FormatInt(v.ID, 10),
			Label: strings.Join(clean, " / "),
		})
	}

	raw, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}

	// 這段字串會被塞進 data-options="..." 屬性，因此雙引號必須轉成實體，
	// 否則會提前關掉屬性、讓 HTML 變成壞的。
	// （json.Marshal 本身已把字串內容的 " 轉成 \"，這裡處理的是結構用的 "。）
	out := strings.ReplaceAll(string(raw), `"`, "&quot;")
	return strings.ReplaceAll(out, "'", "&#39;")
}
