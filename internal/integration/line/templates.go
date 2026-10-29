package line

import (
	"fmt"
)

// EstimateMessage 供推播估價單用，含明細行與金額。
type EstimateMessage struct {
	Title   string
	SubText string
	Items   []ItemLine
	Total   float64
	// ApproveURL 線上核准連結（車行 → 客戶推播單向，可選）。
	ApproveURL string
}

// ToFlex 把 EstimateMessage 轉成 Flex Message 結構供 client.SendFlexMessage。
func (m EstimateMessage) ToFlex() map[string]interface{} {
	contents := []map[string]interface{}{
		{
			"type": "box",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"separator": true,
				"layout": map[string]interface{}{
					"type": "VBOX",
					"contents": []map[string]interface{}{
						{
							"type": "text",
							"tag":  "fill",
							"spec": map[string]interface{}{
								"text":   m.Title,
								"size":   "md",
								"color":  "#008527",
								"weight": "bold",
							},
						},
						{
							"type": "text",
							"tag":  "fill",
							"spec": map[string]interface{}{
								"text":  m.SubText,
								"size":  "sm",
								"color": "#003838",
							},
						},
					},
				},
			},
		},
		{
			"type": "separator",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"layout": map[string]interface{}{
					"type":     "VBOX",
					"contents": flexRows(m.Items),
				},
			},
		},
		{
			"type": "separator",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"layout": map[string]interface{}{
					"type": "VBOX",
					"contents": []map[string]interface{}{
						{
							"type": "text",
							"tag":  "fill",
							"spec": map[string]interface{}{
								"text":   fmt.Sprintf("總計：%s", money(m.Total)),
								"size":   "md",
								"color":  "#008527",
								"weight": "bold",
							},
						},
					},
				},
			},
		},
	}
	if m.ApproveURL != "" {
		contents = append(contents, map[string]interface{}{
			"type": "button",
			"tag":  "fill",
			"spec": map[string]interface{}{
				"mode": "uri",
				"url":  m.ApproveURL,
				"text": "線上核准",
			},
		})
	}
	return map[string]interface{}{
		"type":     "flex",
		"altText":  m.Title,
		"contents": contents,
	}
}

// flexRows 把 []ItemLine 排成 VBOX 兩欄布局。
func flexRows(items []ItemLine) []map[string]interface{} {
	rows := []map[string]interface{}{}
	for _, it := range items {
		rows = append(rows, map[string]interface{}{
			"separator": false,
			"margin": map[string]interface{}{
				"top":    "sm",
				"right":  "sm",
				"bottom": "sm",
				"left":   "sm",
			},
			"align": "spaceBetween",
			"contents": []map[string]interface{}{
				{"type": "text", "tag": "fill", "spec": map[string]interface{}{
					"text": it.Desc, "color": "#003838", "size": "sm", "wrap": "wrap",
				}},
				{"type": "text", "tag": "fill", "spec": map[string]interface{}{
					"text": money(it.Total), "color": "#008527", "size": "sm", "weight": "bold",
				}},
			},
		})
	}
	return rows
}

// money 把金額格式化成 1,234.50 形式，供 Flex/文本顯示。
func money(v float64) string {
	return fmt.Sprintf("%.2f", v)
}
