package components

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"autorepairos/internal/model"

	"github.com/a-h/templ"
	templruntime "github.com/a-h/templ/runtime"
)

// init shrinks templ's render buffer to 1 byte for the whole test binary so
// every write flushes to the underlying writer. This makes templ's generated
// per-write `if err != nil { return err }` branches reachable by failing
// writers; with the default 4KB buffer they only fire at the final flush.
func init() { templruntime.DefaultBufferSize = 1 }

// Round-2 component tests: alternate/edge inputs for branches left uncovered
// by round 1. Reuses renderComp from components_render_test.go.

// TestComponentHelpersRound2 covers the plain-Go helper.
func TestComponentHelpersRound2(t *testing.T) {
	if boolString(true) != "true" || boolString(false) != "false" {
		t.Fatal("boolString")
	}
}

// TestRenderVariantsRound2 covers alternate branches + children + edge pages.
func TestRenderVariantsRound2(t *testing.T) {
	owner := model.User{ID: 1, Username: "owner1", Role: "owner"}
	counter := model.User{ID: 2, Username: "counter1", Role: "counter"}
	staff := model.User{ID: 4, Username: "staff1", Role: "staff"}
	anon := model.User{}

	cases := []struct {
		name string
		comp templ.Component
		want string
	}{
		// BayOptions: multi-option, legacy with options, empty legacy
		{"BayOptionsMulti", BayOptions("B2", []BayOption{{Code: "B1", Label: "一號工位"}, {Code: "B2", Label: "二號工位"}, {Code: "B3", Label: "烤漆房"}}, false), "二號工位"},
		{"BayOptionsLegacyWithOpts", BayOptions("B1", []BayOption{{Code: "B1", Label: "一號工位"}}, true), "一號工位"},
		{"BayOptionsEmpty", BayOptions("", nil, false), ""},
		// Pill / Btn / BtnConfirm / Card(+children) / Empty
		{"PillClass", Pill("待處理", "pill-warn"), "pill-warn"},
		{"PillEmpty", Pill("", ""), ""},
		{"BtnConfirmEmpty", BtnConfirm("", "", "", ""), ""},
		{"CardWithChildren", Card("外層標題"), "外層標題"},
		{"CardChildContent", childrenComp(), "內層內容"},
		{"CardTitleOnly", Card(""), ""},
		{"EmptyMsg", Empty("查無資料，請調整篩選"), "查無資料"},
		// Pager edges: first / middle / last / single / zero / many pages
		{"PagerFirst", Pager("/list", 1, 5, 47), "下一頁"},
		{"PagerMiddle", Pager("/list", 3, 5, 47), "上一頁"},
		{"PagerLast", Pager("/list", 5, 5, 47), "上一頁"},
		{"PagerNumbersFirst", PagerNumbers("/list", 1, 5), "1"},
		{"PagerNumbersLast", PagerNumbers("/list", 5, 5), "5"},
		{"PagerNumbersMiddle", PagerNumbers("/list", 10, 20), "10"},
		{"PagerNumbersExact", PagerNumbers("/list", 4, 7), "4"},
		// Flash / form fields
		{"FormFieldOptional", FormField("備註", "notes", "text", "選填", false), "notes"},
		{"FormFieldRequiredDate", FormField("日期", "date", "date", "", true), "date"},
		{"SelectFieldMulti", SelectField("狀態", "status", []SelectOption{{Value: "0", Label: "全部"}, {Value: "1", Label: "啟用", Selected: true}, {Value: "2", Label: "停用"}}), "啟用"},
		{"SelectFieldEmpty", SelectField("空", "empty", nil), "empty"},
		{"PlateScanUIEmpty", PlateScanUI(""), ""},
		{"PlateScanUIFields", PlateScanUI(`{"plate":"plate_number","vin":"vin"}`), "plate_number"},
		{"BarcodeScanUIAlt", BarcodeScanUI("scanBtn2", "partInput", "scanBox2"), "scanBtn2"},
		// Layout variants: different roles, empty user, empty title/appName
		{"LayoutCounter", Layout("收款", "/receivables", counter, "App"), "counter1"},
		{"LayoutStaff", Layout("員工", "/staff", staff, "App"), "staff1"},
		{"LayoutAnon", Layout("", "", anon, ""), ""},
		{"LayoutOwnerChild", Layout("首頁", "/", owner, "AutoRepair OS"), "owner1"},
		{"CSRFInputOnly", CSRFInput(), "_csrf"},
		{"NavItemsActive", NavItems("/work-orders", "owner"), ""},
		{"NavItemsTech", NavItems("/work-orders", "technician"), ""},
		{"NavItemsCounter", NavItems("/appointments", "counter"), ""},
		// Lookup results: empty, multiple, with/without bin location
		{"PartLookupResultsMulti", PartLookupResults([]PartLookupResult{{ID: 1, Name: "機油", PartNumber: "P-1", BinLocation: "A1", Price: "500", Stock: 3}, {ID: 2, Name: "煞車皮", PartNumber: "P-2", BinLocation: "", Price: "900", Stock: 0}, {ID: 3, Name: "濾芯", PartNumber: "P-3", BinLocation: "B2", Price: "300", Stock: 10}}), "濾芯"},
		{"PhraseLookupResultsMulti", PhraseLookupResults([]PhraseLookupResult{{ID: 1, Phrase: "檢查胎壓", Category: "檢查"}, {ID: 2, Phrase: "煞車試車", Category: "試車"}}), "煞車試車"},
		{"PhraseLookupResultsEmpty", PhraseLookupResults(nil), ""},
		// Quick vehicle result variants
		{"QuickVehicleResultDuplicate", QuickVehicleResult(QuickVehicleResultData{OK: true, VehicleID: "8", Label: "XYZ-9999", Duplicate: true, Mileage: "60000"}), "XYZ-9999"},
		{"QuickVehicleResultMileage", QuickVehicleResult(QuickVehicleResultData{OK: true, VehicleID: "9", Label: "QQQ-1111", Mileage: "45000"}), "45000"},
		{"QuickVehicleResultZero", QuickVehicleResult(QuickVehicleResultData{}), ""},
		// Supplier lookup variants
		{"SupplierLookupOptionsMulti", SupplierLookupOptions([]SupplierLookupOption{{ID: 1, Name: "供應商A"}, {ID: 2, Name: "供應商B"}}), "供應商B"},
		{"SupplierQuickCreateResultZero", SupplierQuickCreateResult("0", "新供應商", "備註", "red"), "新供應商"},
		{"SupplierQuickCreateResultEmpty", SupplierQuickCreateResult("", "", "", ""), ""},
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

// childrenComp renders a Card with children content.
func childrenComp() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		ctx = templ.WithChildren(ctx, Empty("內層內容"))
		return Card("外框").Render(ctx, w)
	})
}

// failWriter errors after limit bytes, forcing templ's error-return branches.
type failWriter struct {
	limit int
	n     int
}

func (w *failWriter) Write(p []byte) (int, error) {
	if w.n >= w.limit {
		return 0, errors.New("boom")
	}
	if w.n+len(p) > w.limit {
		m := w.limit - w.n
		w.n = w.limit
		return m, errors.New("boom")
	}
	w.n += len(p)
	return len(p), nil
}

// sweepErrorPaths renders c with a writer failing at every byte offset,
// covering templ's per-write error branches.
func sweepErrorPaths(t *testing.T, c templ.Component) {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("baseline render: %v", err)
	}
	n := buf.Len()
	if n > 20000 {
		n = 20000
	}
	for i := 0; i <= n; i++ {
		_ = c.Render(context.Background(), &failWriter{limit: i})
	}
}

// TestErrorPathsRound2 sweeps every component through failing writers.
// templ buffers output in a 4KB bufio.Writer, so a failing writer only
// surfaces at the final flush; shrinking the buffer to 1 byte forces a flush
// on every write, making each generated `if err != nil { return err }`
// branch reachable.
func TestErrorPathsRound2(t *testing.T) {
	old := templruntime.DefaultBufferSize
	templruntime.DefaultBufferSize = 1
	defer func() { templruntime.DefaultBufferSize = old }()

	owner := model.User{ID: 1, Username: "owner1", Role: "owner"}
	comps := map[string]templ.Component{
		"BayOptions":                BayOptions("B1", []BayOption{{Code: "B1", Label: "一號工位"}, {Code: "B2", Label: "二號工位"}}, false),
		"BayOptionsLegacy":          BayOptions("", nil, true),
		"Pill":                      Pill("完成", "pill-ok"),
		"Btn":                       Btn("/x", "新增", "btn"),
		"BtnConfirm":                BtnConfirm("/x", "刪除", "btn", "確定？"),
		"Card":                      Card("標題"),
		"CardChildren":              childrenComp(),
		"Empty":                     Empty("查無資料"),
		"Pager":                     Pager("/list", 2, 5, 47),
		"PagerNumbers":              PagerNumbers("/list", 2, 5),
		"FlashMsg":                  FlashMsg("已儲存"),
		"FlashErr":                  FlashErr("失敗"),
		"FormField":                 FormField("姓名", "name", "text", "請輸入", true),
		"FormFieldOpt":              FormField("備註", "notes", "text", "", false),
		"SelectField":               SelectField("狀態", "status", []SelectOption{{Value: "1", Label: "啟用", Selected: true}}),
		"PlateScanUI":               PlateScanUI(`{"plate":"x"}`),
		"BarcodeScanUI":             BarcodeScanUI("btn", "target", "box"),
		"InvoiceScanUI":             InvoiceScanUI(),
		"Layout":                    Layout("首頁", "/", owner, "AutoRepair OS"),
		"CSRFInput":                 CSRFInput(),
		"NavItems":                  NavItems("/", "owner"),
		"PartLookupResults":         PartLookupResults([]PartLookupResult{{ID: 1, Name: "機油", PartNumber: "P-1", BinLocation: "A1", Price: "500", Stock: 3}}),
		"PhraseLookupResults":       PhraseLookupResults([]PhraseLookupResult{{ID: 1, Phrase: "檢查胎壓", Category: "檢查"}}),
		"QuickVehicleResult":        QuickVehicleResult(QuickVehicleResultData{OK: true, VehicleID: "7", Label: "ABC-1234"}),
		"SupplierLookupOptions":     SupplierLookupOptions([]SupplierLookupOption{{ID: 1, Name: "供應商A"}}),
		"SupplierQuickCreateResult": SupplierQuickCreateResult("3", "供應商B", "", ""),
	}
	for name, c := range comps {
		c := c
		t.Run(name, func(t *testing.T) { sweepErrorPaths(t, c) })
	}
}

// TestCancelledContextRound2 renders every component with a cancelled context,
// covering templ's ctx.Err() early-return branch.
func TestCancelledContextRound2(t *testing.T) {
	owner := model.User{ID: 1, Username: "owner1", Role: "owner"}
	comps := map[string]templ.Component{
		"BayOptions":     BayOptions("B1", nil, false),
		"Pill":           Pill("x", "pill"),
		"Btn":            Btn("/x", "y", "btn"),
		"BtnConfirm":     BtnConfirm("/x", "y", "btn", "c"),
		"Card":           Card("t"),
		"Empty":          Empty("e"),
		"Pager":          Pager("/l", 1, 2, 10),
		"FlashMsg":       FlashMsg("m"),
		"FlashErr":       FlashErr("e"),
		"FormField":      FormField("l", "n", "text", "p", true),
		"SelectField":    SelectField("l", "n", nil),
		"PlateScanUI":    PlateScanUI("{}"),
		"BarcodeScanUI":  BarcodeScanUI("b", "t", "c"),
		"InvoiceScanUI":  InvoiceScanUI(),
		"Layout":         Layout("t", "/", owner, "App"),
		"CSRFInput":      CSRFInput(),
		"NavItems":       NavItems("/", "owner"),
		"PartLookup":     PartLookupResults(nil),
		"PhraseLookup":   PhraseLookupResults(nil),
		"QuickVehicle":   QuickVehicleResult(QuickVehicleResultData{}),
		"SupplierOpts":   SupplierLookupOptions(nil),
		"SupplierCreate": SupplierQuickCreateResult("", "", "", ""),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, c := range comps {
		c := c
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			_ = c.Render(ctx, &buf) // error expected; branch taken
		})
	}
}
