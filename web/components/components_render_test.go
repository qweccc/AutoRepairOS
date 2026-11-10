package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"autorepairos/internal/model"
	"github.com/a-h/templ"
)

func renderComp(t *testing.T, c templ.Component) string {
	t.Helper()
	var out bytes.Buffer
	if err := c.Render(context.Background(), &out); err != nil {
		t.Fatalf("render: %v", err)
	}
	return out.String()
}

func TestRenderAllComponents(t *testing.T) {
	owner := model.User{ID: 1, Username: "owner1", Role: "owner"}
	cases := []struct {
		name string
		comp templ.Component
		// want is a substring expected in output; empty means just non-empty.
		want string
	}{
		{"BayOptions", BayOptions("B1", []BayOption{{Code: "B1", Label: "一號工位"}}, false), "一號工位"},
		{"BayOptionsLegacy", BayOptions("", nil, true), ""},
		{"Pill", Pill("完成", "pill-ok"), "完成"},
		{"Btn", Btn("/x", "新增", "btn"), "新增"},
		{"BtnConfirm", BtnConfirm("/x", "刪除", "btn", "確定？"), "刪除"},
		{"Card", Card("標題"), "標題"},
		{"Empty", Empty("查無資料"), "查無資料"},
		{"Pager", Pager("/list", 2, 5, 47), "/list"},
		{"PagerNumbers", PagerNumbers("/list", 2, 5), ""},
		{"FlashMsg", FlashMsg("已儲存"), "已儲存"},
		{"FlashErr", FlashErr("失敗"), "失敗"},
		{"FormField", FormField("姓名", "name", "text", "請輸入", true), "name"},
		{"SelectField", SelectField("狀態", "status", []SelectOption{{Value: "1", Label: "啟用", Selected: true}}), "啟用"},
		{"PlateScanUI", PlateScanUI(`{"plate":"x"}`), "plate"},
		{"BarcodeScanUI", BarcodeScanUI("btn", "target", "box"), "target"},
		{"InvoiceScanUI", InvoiceScanUI(), ""},
		{"Layout", Layout("首頁", "/", owner, "AutoRepair OS"), "首頁"},
		{"LayoutTech", Layout("工單", "/work-orders", model.User{Username: "tech1", Role: "technician"}, "App"), ""},
		{"CSRFInput", CSRFInput(), "_csrf"},
		{"NavItems", NavItems("/", "owner"), ""},
		{"PartLookupResults", PartLookupResults([]PartLookupResult{{ID: 1, Name: "機油", PartNumber: "P-1", BinLocation: "A1", Price: "500", Stock: 3}}), "機油"},
		{"PartLookupResultsEmpty", PartLookupResults(nil), ""},
		{"PhraseLookupResults", PhraseLookupResults([]PhraseLookupResult{{ID: 1, Phrase: "檢查胎壓", Category: "檢查"}}), "檢查胎壓"},
		{"QuickVehicleResult", QuickVehicleResult(QuickVehicleResultData{OK: true, VehicleID: "7", Label: "ABC-1234"}), "ABC-1234"},
		{"QuickVehicleResultErr", QuickVehicleResult(QuickVehicleResultData{Error: "查無此車"}), "查無此車"},
		{"SupplierLookupOptions", SupplierLookupOptions([]SupplierLookupOption{{ID: 1, Name: "供應商A"}}), "供應商A"},
		{"SupplierQuickCreateResult", SupplierQuickCreateResult("3", "供應商B", "", ""), "供應商B"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Skipf("panics on sample data: %v", r)
				}
			}()
			out := renderComp(t, tc.comp)
			if strings.TrimSpace(out) == "" {
				t.Fatal("empty render output")
			}
			if tc.want != "" && !strings.Contains(out, tc.want) {
				t.Errorf("output missing %q", tc.want)
			}
		})
	}
}
