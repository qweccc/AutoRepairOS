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
// 回傳未做 HTML 轉義的 JSON；由 Templ 在 data-options attribute 中負責轉義。
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
			clean = append(clean, strings.TrimSpace(p))
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

	return string(raw)
}
