package line

import (
	"strings"
	"testing"
)

func TestEstimateToFlexWithURL(t *testing.T) {
	m := EstimateMessage{
		Title: "估價單", SubText: "請核准",
		Items:      []ItemLine{{Desc: "機油", Total: 650}, {Desc: "濾芯", Total: 120}},
		Total:      770,
		ApproveURL: "http://approve/1",
	}
	f := m.ToFlex()
	if f["type"] != "flex" || f["altText"] != "估價單" {
		t.Errorf("flex = %v", f)
	}
	contents, ok := f["contents"].([]map[string]interface{})
	if !ok || len(contents) != 4 {
		t.Fatalf("contents len = %d", len(contents))
	}
	if contents[3]["type"] != "button" {
		t.Errorf("最後應是核准按鈕: %v", contents[3])
	}
}

func TestEstimateToFlexNoURL(t *testing.T) {
	m := EstimateMessage{Title: "t", Items: nil, Total: 0}
	f := m.ToFlex()
	contents := f["contents"].([]map[string]interface{})
	if len(contents) != 3 {
		t.Errorf("無 ApproveURL 應有 3 個區塊, got %d", len(contents))
	}
}

func TestFlexRows(t *testing.T) {
	if rows := flexRows(nil); len(rows) != 0 {
		t.Errorf("空 items 應回空, got %v", rows)
	}
	rows := flexRows([]ItemLine{{Desc: "機油", Total: 100}})
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	s := strings.TrimSpace("")
	_ = s
	contents := rows[0]["contents"].([]map[string]interface{})
	if len(contents) != 2 {
		t.Errorf("每列應有 描述+金額 兩欄")
	}
}
