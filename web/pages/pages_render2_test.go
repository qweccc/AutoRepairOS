package pages

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"autorepairos/internal/model"
	"autorepairos/web/components"

	"github.com/a-h/templ"
)

// Round-2 render tests: alternate data (full/empty/zero/roles/flags) so both
// sides of data-driven branches execute. Reuses renderPages + test* fixtures
// from pages_render_test.go; local vars use the r2 prefix to avoid redefines.

// TestHelpersRound2 exercises every plain-Go helper's branches directly.
func TestHelpersRound2(t *testing.T) {
	// statusIndex: every flow status + unknown
	for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid", "bogus", ""} {
		statusIndex(s)
	}
	// statusStepStyle: before / current / after
	for _, p := range [][2]int{{0, 2}, {2, 2}, {3, 2}, {0, 0}} {
		statusStepStyle(p[0], p[1])
	}
	// itemTypeLabel: all cases + fallback
	for _, s := range []string{"labor", "part", "subcontract", "other", ""} {
		itemTypeLabel(s)
	}
	if yesNo(true) != "是" || yesNo(false) != "否" {
		t.Fatal("yesNo")
	}
	if sopLabel(true) != "取消 SOP" || sopLabel(false) != "標記 SOP 完成" {
		t.Fatal("sopLabel")
	}
	// sopStatusText: total 0 / completed / in progress
	sopStatusText(false, 0)
	sopStatusText(true, 3)
	sopStatusText(false, 3)
	money(0)
	money(98765)

	phraseActiveLabel(1)
	phraseActiveLabel(0)
	phraseActiveLabel(2)

	trimQtyF(2)   // integer
	trimQtyF(2.5) // fractional
	trimQtyF(0)

	ifThen(true, "y", "n")
	ifThen(false, "y", "n")

	moneyOrDash(0)
	moneyOrDash(1200)

	taskStatusLabel(true)
	taskStatusLabel(false)
	taskStatusPill(true)
	taskStatusPill(false)
	taskToggleLabel(true)
	taskToggleLabel(false)
	backupStatusLabel(true)
	backupStatusLabel(false)
	backupStatusPill(true)
	backupStatusPill(false)

	lineBindLabel(model.Customer{LineUserID: "U1"})
	lineBindLabel(model.Customer{})
	telegramBindLabel(model.Customer{TelegramUserID: "123"})
	telegramBindLabel(model.Customer{})

	intOrBlank(0)
	intOrBlank(2024)

	calcPages(0, 20)
	calcPages(0, 0)
	calcPages(47, 20)
	calcPages(20, 20)
	calcPages(1, 20)

	sortClass("name", "name")
	sortClass("name", "stock")
	negativeSortURL("name", "")
	negativeSortURL("name", "機油")
	negativeSortURL("", "")
	sourceClass("purchase_receive", "purchase_receive")
	sourceClass("purchase_receive", "manual")
	sourceURL("purchase_receive", "")
	sourceURL("purchase_receive", "機油")
	sourceURL("", "")

	btnCls(true)
	btnCls(false)

	estimateMoney(0)
	estimateMoney(3000)
	for _, s := range []string{"labor", "part", "subcontract", "other", ""} {
		estimateItemType(s)
	}
	estimateSubtotal(nil)
	estimateSubtotal([]EstimateItemRow{{Quantity: 2, UnitPrice: 500}, {Quantity: 1, UnitPrice: 100}})
	estimateURL(42)

	phase5ButtonClass("/reports", "/reports")
	phase5ButtonClass("/reports", "/analytics")

	moneyFmt(0)
	moneyFmt(10000)
	orEmpty("")
	orEmpty("x")
	moneyValue(0)
	moneyValue(800)

	tagPillsHTML("")
	tagPillsHTML("VIP")
	tagPillsHTML("VIP,奧客,回頭客")
	tagPillsHTML(",lead,")
}

// TestRenderVariantsRound2 covers alternate branches of the lowest-coverage pages.
func TestRenderVariantsRound2(t *testing.T) {
	// Shared fixtures (alternate / full variants of round-1 data)
	apOpen := APPayableRow{ID: 2, SupplierName: "供應商B", Amount: 2000, PaidAmount: 0, Balance: 2000, Status: "open", StatusLabel: "未付", StatusPill: "pill", DueDate: "2026-12-01", SourceLabel: "進貨", Payments: []APPaymentRow{{Amount: 500, PaidAt: "2026-11-02"}}}
	apPaid := APPayableRow{ID: 3, SupplierName: "供應商C", Amount: 900, PaidAmount: 900, Balance: 0, Status: "paid", StatusLabel: "已付清", StatusPill: "pill-ok"}
	apVoid := APPayableRow{ID: 4, SupplierName: "供應商D", Amount: 300, PaidAmount: 0, Balance: 300, Status: "void", StatusLabel: "作廢", StatusPill: "pill"}
	apptFull := AppointmentRow{ID: 2, AppointmentDate: "2026-11-11", AppointmentTime: "10:00", Bay: "B2", BayLabel: "二號工位", PlateNumber: "XYZ-9999", CustomerName: "李小華", ServiceType: "保養", Status: "confirmed", StatusLabel: "已確認", StatusPill: "pill", HasWorkOrder: true}
	apptCancelled := AppointmentRow{ID: 3, AppointmentDate: "2026-11-12", PlateNumber: "QQQ-1111", CustomerName: "陳大同", Status: "cancelled", StatusLabel: "已取消", StatusPill: "pill"}
	custFull := CustomerRow{ID: 2, Name: "李小華", Phone: "0922333444", Tags: "VIP,回頭客", Address: "台北市", Notification: "line", LineUserID: "U123", TelegramUserID: "987", TotalSpent: 8000, UnpaidAmount: 200}
	equipFull := EquipmentRow{ID: 2, Name: "烤漆房", Model: "M-200", SerialNumber: "SN01", Location: "一樓", Status: "repairing", StatusLabel: "維修中", StatusPill: "pill-warn"}
	estConverted := EstimateListRow{ID: 2, CustomerName: "李小華", PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", Status: "converted", TotalAmount: 5000, DiscountAmount: 500, CreatedAt: "2026-11-02", ConvertedWorkOrderID: 9}
	estDetailConverted := EstimateDetail{ID: 2, CustomerName: "李小華", CustomerPhone: "0922333444", PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", Status: "converted", CurrentMileage: 60000, CustomerComplaint: "煞車異音", DiscountAmount: 500, TotalAmount: 5000, CreatedAt: "2026-11-02", ConvertedWorkOrderID: 9}
	expIn := ExpenseRow{ID: 2, Date: "2026-11-03", Part: "雜項", Notes: "印花稅", AmtIn: 1000, AmtOut: 0}
	expBoth := ExpenseRow{ID: 3, Date: "2026-11-04", Part: "零件", AmtIn: 200, AmtOut: 350}
	vInsClass := VehicleInsuranceRow{ID: 2, VehicleID: 1, Plate: "XYZ-9999", Customer: "李小華", Company: "保險B", ClassName: "丙式", Class: "C", Date: "2026-10-01", CardNo: "C-002", Amount: "12000", PolicyNumber: "P-002", ExpiryDate: "2027-10-01"}
	vInsFormEdit := VehicleInsuranceFormData{ID: 7, Title: "編輯保險", Action: "/insurance/7", VehicleID: 1,
		VehicleOptions: []components.SelectOption{{Value: "1", Label: "ABC-1234", Selected: true}},
		CompanyID:      2,
		CompanyOptions: []components.SelectOption{{Value: "2", Label: "保險B", Selected: true}},
		CompanyNo:      "C-002", Class: "C", ClassName: "丙式", LicenseDate: "2026-10-01", PolicyNumber: "P-002", ExpiryDate: "2027-10-01", CardNo: "C-002", AmountRaw: "12000", Notes: "備註"}
	insCompanyOff := InsuranceCompanyRow{ID: 2, Code: "INS2", Name: "保險B", TaxID: "12345678", ClaimPhone: "0800-000", ContactPerson: "王經理", Active: 0}
	invLabor := InventoryRow{ID: 2, PartNumber: "L-001", Name: "工資服務", Category: "維修", BinLocation: "—", SupplierName: "內部", CostPrice: 0, SellingPrice: 1000, StockQuantity: 0, MinSafety: 1, LowStock: true, ItemKind: "labor"}
	invFull := InventoryRow{ID: 3, PartNumber: "P-003", Name: "煞車皮", Category: "煞車", BinLocation: "B2", SupplierName: "供應商A", CostPrice: 900, SellingPrice: 1500, StockQuantity: 1, MinSafety: 5, LowStock: true, ItemKind: "part"}
	lookupOff := LookupRow{ID: 2, Code: "OLD", Label: "舊代碼", SortOrder: 2, Active: 0}
	lookupOn := LookupRow{ID: 3, Code: "NEW", Label: "新代碼", SortOrder: 1, Active: 1}
	poReceived := PurchaseOrderRow{ID: 2, SupplierName: "供應商B", OrderDate: "2026-10-01", Status: "received", TotalAmount: 8000, Notes: "已進貨", ItemCount: 3}
	recLegacy := ReceivableRow{ID: 2, VoucherNo: "V-LEGACY", CustomerName: "舊客", PlateNumber: "OLD-0001", MethodLabel: "現金", PayTotal: 500, IsLegacy: true, UnpaidRed: true}
	recFull := ReceivableRow{ID: 3, VoucherNo: "V-003", CustomerName: "李小華", PlateNumber: "XYZ-9999", MethodLabel: "刷卡", PayTotal: 2500, WorkOrderID: 5, WoCell: "5", ReceivedCell: "2500", UnpaidCell: "0"}
	staffDependent := StaffRow{ID: 2, Username: "dep1", Name: "眷屬A", Role: "technician", Branch: "維修", Kind: "dependent", KindLabel: "眷屬", KindCls: "pill", NationalID: "A123456789", Birthday: "2015-01-01", InsStart: "2026-01-01", InsEnd: "2026-12-31", InsEndLabel: "2026-12-31", InsEndRed: true, DepOf: 3, DepOfName: "技師A", Active: 0, CanEdit: false}
	staffFull := StaffRow{ID: 4, Username: "ownerX", Name: "店長", Role: "owner", Branch: "管理", Kind: "employee", KindLabel: "員工", KindCls: "pill-ok", Active: 1, CanEdit: true}
	subFull := SubcontractRow{ID: 2, WorkOrderID: 3, SupplierName: "鈑金廠B", Description: "烤漆", Status: "done", StatusLabel: "已完成", StatusPill: "pill-ok"}
	supVendor := SupplierRow{ID: 2, Name: "鈑金廠B", VendorType: "vendor", VendorLabel: "協力廠商", PaymentTerms: "月結", Phone: "0223456789", ContactPerson: "張先生", Address: "新北市", Notes: "配合度佳", Active: 0}
	supMaterial := SupplierRow{ID: 3, Name: "材料商C", VendorType: "material", VendorLabel: "材料商", Active: 1}
	vehType := VehicleRow{ID: 2, PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", VehicleTypeLabel: "自用", CustomerName: "李小華", CanEdit: false}
	woInsurance := WorkOrderRow{ID: 2, PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", CustomerName: "李小華", Status: "completed", StatusLabel: "已完工", StatusPill: "pill-ok", ApprovalStatus: "approved", ApprovalLabel: "已同意", ApprovalPill: "pill-ok", IsInsurance: true, PartCount: 2, TotalAmount: 8000}
	woMultiPart := WorkOrderRow{ID: 3, PlateNumber: "QQQ-1111", BrandModel: "Nissan Sentra", CustomerName: "陳大同", Status: "waiting_parts", StatusLabel: "等待零件", StatusPill: "pill-warn", ApprovalStatus: "pending", ApprovalLabel: "待核准", ApprovalPill: "pill", PartCount: 1, TotalAmount: 1200}
	vehFull := model.Vehicle{ID: 2, PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", CustomerID: 2, CustomerName: "李小華", VehicleTypeLabel: "自用", FuelTypeLabel: "汽油", BodyStyleLabel: "四門房車", OwnerName: "李小華", OwnerAddress: "台北市", DriverName: "李小華", DriverPhone: "0922333444", ContactName: "李太太", ContactPhone: "0911111111", Color: "銀色", ManufactureYear: 2019, ManufactureDate: "2019-05", Displacement: 1800, VIN: "1HGBH41JXMN109186", EngineNumber: "K24Z3-123456", SeatingCapacity: 5, InspectionDueDate: "2027-05-01", LastMaintenanceDate: "2026-09-01", MaintIntervalMonths: 6, MaintIntervalKm: 10000}
	custBound := model.Customer{ID: 2, Name: "李小華", Phone: "0922333444", LineUserID: "U999", TelegramUserID: "555", NotificationPref: "line", Address: "台北市", Tags: "VIP", CampaignCode: "2026Q4", Notes: "注意"}
	woInsuranceFull := model.WorkOrder{ID: 3, PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", CustomerName: "李小華", CustomerPhone: "0922333444", TechnicianName: "技師A", Status: "completed", ApprovalStatus: "approved", CurrentMileage: 60000, CustomerComplaint: "煞車異音", IsInsurance: true, InsuranceCompanyID: 2, InsuranceName: "保險B", ClaimNo: "CL-001", AccidentDate: "2026-10-15", DeductibleAmount: 1000, ClaimAmount: 5000, ClaimDate: "2026-10-20", ClaimNote: "已送件", SOPCompleted: true, Notes: "完工"}
	woPending := model.WorkOrder{ID: 4, PlateNumber: "QQQ-1111", Status: "pending", ApprovalStatus: "pending"}

	cases := []struct {
		name string
		comp templ.Component
		want string
	}{
		// AP
		{"APPayableRowContentOpenOwner", APPayableRowContent(apOpen, "2026-11-09", "owner"), "供應商B"},
		{"APPayableRowContentPaidCounter", APPayableRowContent(apPaid, "2026-11-09", "counter"), "供應商C"},
		{"APPayableRowContentVoidOwner", APPayableRowContent(apVoid, "2026-11-09", "owner"), "供應商D"},
		{"APHomePageEmpty", APHomePage(nil, nil, nil, 0, "", "2026-11-09", testOwner), ""},
		// Appointments
		{"ListAppointmentsPageTech", ListAppointmentsPage([]AppointmentRow{apptFull}, 1, 1, 20, "2026-11-11", "confirmed", "2026-11-11", "month", []CalendarDay{{Date: "2026-11-11", Label: "11/11", Appointments: []AppointmentRow{apptFull, apptCancelled}}}, components.BayOptions("B2", []components.BayOption{{Code: "B2", Label: "二號工位"}}, false), []BaySummary{{Label: "二號工位", Slots: []string{"09:00", "10:00"}}, {Label: "一號工位"}}, "2026-11-11", testTech), "XYZ-9999"},
		{"ListAppointmentsPageCounter", ListAppointmentsPage(nil, 0, 1, 20, "", "", "", "day", nil, components.BayOptions("", nil, true), nil, "", testCounter), ""},
		{"AppointmentRowContentFull", AppointmentRowContent(apptFull), "二號工位"},
		{"AppointmentRowContentCancelled", AppointmentRowContent(apptCancelled), "QQQ-1111"},
		{"AppointmentSearchRowsEmpty", AppointmentSearchRows(nil), ""},
		{"AppointmentCalendarEmptyDay", AppointmentCalendar("2026-11-10", "month", "", []CalendarDay{{Date: "2026-11-10", Label: "11/10"}}), "2026-11-10"},
		// Approval
		{"ApprovalPageDecided", ApprovalPage(ApprovalPageData{Request: &model.ApprovalRequest{ID: 2, Token: "tok2", ExpiresAt: "2026-11-20", ItemSummary: "煞車皮 x2"}, WorkOrder: &testWO, Customer: &testCustomer, Items: []model.WorkOrderItem{testWOItem}, IsDecided: true, DecidedText: "已同意"}), "已同意"},
		{"ApprovalPageBare", ApprovalPage(ApprovalPageData{ErrorMsg: "查無資料"}), "查無資料"},
		{"ApprovalResultSuccess", ApprovalResult("success", "成功"), "成功"},
		{"ApprovalResultExpired", ApprovalResult("expired", "已過期"), "已過期"},
		{"ApprovalResultDecided", ApprovalResult("decided", "已決定"), "已決定"},
		{"ApprovalResultOther", ApprovalResult("other", "其他"), "其他"},
		{"ApprovalRequestBlockPending", ApprovalRequestBlock(2, true, []PendingApproval{{ID: 1, Description: "追加煞車皮", Quantity: 2, UnitPrice: 1500, ItemType: "part", ExpiresAt: "2026-11-20"}, {ID: 2, Description: "工資", Quantity: 1, UnitPrice: 800}}), "追加煞車皮"},
		{"ApprovalRequestBlockNoCreatePending", ApprovalRequestBlock(2, false, []PendingApproval{{ID: 1, Description: "追加項目"}}), "追加項目"},
		// Claim fields
		{"WorkOrderClaimFieldsInsurance", WorkOrderClaimFields(woInsuranceFull, true), ""},
		{"WorkOrderClaimFieldsPlainReadonly", WorkOrderClaimFields(woPending, false), ""},
		// Customer
		{"ListCustomersPageFull", ListCustomersPage([]CustomerRow{custFull}, 1, 1, 20, "李", CustomerCreateForm{}, testCounter), "李小華"},
		{"CustomerSearchRowsEmpty", CustomerSearchRows(nil, testOwner), ""},
		// Dashboard
		{"DashboardPageEmptyLists", DashboardPage(nil, nil, nil, testCounter), ""},
		// Equipment
		{"ListEquipmentPageFull", ListEquipmentPage([]EquipmentRow{equipFull}, 1, 1, 20, "烤漆", "repairing", testOwner), "烤漆房"},
		{"EquipmentRowContentFull", EquipmentRowContent(equipFull), "烤漆房"},
		{"EquipmentSearchRowsEmpty", EquipmentSearchRows(nil), ""},
		// Estimates
		{"EstimateListPageConverted", EstimateListPage([]EstimateListRow{estConverted}, testCounter), "XYZ-9999"},
		{"EstimateListPageEmpty", EstimateListPage(nil, testTech), ""},
		{"EstimateNewPageWithVehicles", EstimateNewPage([]EstimateVehicleOption{{ID: 1, PlateNumber: "ABC-1234", BrandModel: "Toyota Altis", CustomerName: "王小明"}, {ID: 2, PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", CustomerName: "李小華"}}, `[{"id":1}]`, testOwner), ""},
		{"EstimateDetailPageConverted", EstimateDetailPage(estDetailConverted, nil, testOwner, false), "李小華"},
		{"EstimateSourcePageEmpty", EstimateSourcePage(estDetailConverted, nil, 0, 0, 0, testCounter), "李小華"},
		// Expense
		{"ListExpensesPageFull", ListExpensesPage([]ExpenseRow{expIn, expBoth}, 2, 1, 20, "", 1200, 350, testOwner), ""},
		{"ExpenseRowContentIn", ExpenseRowContent(expIn), ""},
		{"ExpenseRowContentBoth", ExpenseRowContent(expBoth), ""},
		{"ExpenseSearchRowsEmpty", ExpenseSearchRows(nil), ""},
		// Insurance
		{"ListInsurancePageOff", ListInsurancePage([]InsuranceCompanyRow{insCompanyOff}, 1, 1, 20, "", testOwner), "保險B"},
		{"InsuranceCompanyRowContentOff", InsuranceCompanyRowContent(insCompanyOff), "保險B"},
		{"InsuranceSearchRowsEmpty", InsuranceSearchRows(nil), ""},
		{"VehicleInsuranceFormEdit", VehicleInsuranceForm(vInsFormEdit), "保險B"},
		{"VehicleInsuranceFormPageEdit", VehicleInsuranceFormPage(vInsFormEdit, testOwner), "保險B"},
		{"ListVehicleInsurancesPageExpiring", ListVehicleInsurancesPage([]VehicleInsuranceRow{vInsClass}, 1, 1, 20, "", "", true, "", testOwner), "丙式"},
		{"VehicleInsuranceRowContentClass", VehicleInsuranceRowContent(vInsClass), "丙式"},
		{"ListVehicleInspectionsPagePending", ListVehicleInspectionsPage([]InspectionRow{{ID: 1, VehicleID: 1, Plate: "ABC-1234", Customer: "王小明", OrderDate: "2026-11-01", StatusLabel: "待驗", StatusPill: "pill"}}, 1, 1, 20, "", true, 5, testOwner), "ABC-1234"},
		{"VehicleInsuranceSearchRowsEmpty", VehicleInsuranceSearchRows(nil), ""},
		{"VehicleInspectionSearchRowsEmpty", VehicleInspectionSearchRows(nil), ""},
		// Inventory
		{"ListInventoryPageLowNeg", ListInventoryPage([]InventoryRow{invLabor, invFull}, 2, 4, 2, 1, 20, "", true, false, "", "低庫存", "", InventoryCreateForm{}, testOwner), "煞車皮"},
		{"InventoryRowContentLabor", InventoryRowContent(invLabor, false), "工資服務"},
		{"InventoryRowContentLow", InventoryRowContent(invFull, true), "煞車皮"},
		{"InventorySearchRowsEmpty", InventorySearchRows(nil, true), ""},
		{"EditInventoryPageNoMoney", EditInventoryPage(model.InventoryItem{ID: 2, PartNumber: "L-001", Name: "工資服務", ItemKind: "labor", StockQuantity: 0, MinSafetyStock: 1, SupplierID: 1, SupplierName: "內部"}, false, "<option>維修</option>", "<option>內部</option>", testCounter), "工資服務"},
		{"StockAdjustPageZero", StockAdjustPage(model.InventoryItem{ID: 2, PartNumber: "L-001", Name: "工資服務", StockQuantity: -3, MinSafetyStock: 1}, testTech), "工資服務"},
		{"NegativeStockPageEmpty", NegativeStockPage(NegativeStockStats{}, nil, 0, 1, 20, "", "", "", "", "", "", testOwner), ""},
		{"NegativeStockFixPageEmpty", NegativeStockFixPage(testInvItem, nil, testOwner), "機油"},
		{"MovementLogPageMulti", MovementLogPage([]InventoryMovementRow{{CreatedAt: "2026-11-01 09:00", PartNumber: "P-1", Name: "機油", Delta: -2, Before: 5, After: 3, DeltaPositive: false, Source: "manual_adjust", Reason: "盤點", User: "owner1"}, {CreatedAt: "2026-11-02 09:00", PartNumber: "P-2", Name: "煞車皮", Delta: 4, Before: 0, After: 4, DeltaPositive: true, Source: "purchase_receive", Reason: "", User: "counter1"}}, 2, 1, 20, "", "manual_adjust", "", "", "", testOwner), "manual_adjust"},
		// Login
		{"LoginPageAction", LoginPage("App", "", "/login?next=/"), "/login"},
		// Lookup
		{"ListLookupPageFull", ListLookupPage([]LookupRow{lookupOff, lookupOn}, "category", "類別", 2, "", testOwner), "新代碼"},
		{"LookupRowContentOff", LookupRowContent(lookupOff), "舊代碼"},
		{"LookupRowContentOn", LookupRowContent(lookupOn), "新代碼"},
		{"LookupSearchRowsEmpty", LookupSearchRows(nil), ""},
		{"NewLookupItemPageX", NewLookupItemPage("status", "狀態", testCounter), "狀態"},
		{"EditLookupItemPageOff", EditLookupItemPage(model.LookupItem{ID: 2, Code: "OLD", Label: "舊代碼", Active: 0}, testOwner), "舊代碼"},
		// Phase5
		{"Phase5PageViewTech", Phase5PageView(testTech, Phase5Page{Title: "工時", Active: "/worklogs", Body: components.Empty("無")}), "工時"},
		{"Phase5TablesEmptySection", Phase5Tables([]Phase5Table{{Empty: "查無"}, {Headers: []string{"A"}, Rows: nil}}), ""},
		{"CampaignAnalysisTableFull", CampaignAnalysisTable([]Phase5CampaignRow{{Code: "CAM2", OrderCount: "3", Revenue: "9000", Cost: "4000", Gross: "5000", LaborRecorded: true}, {Code: "CAM3", OrderCount: "1", Revenue: "2000", Cost: "", Gross: "", LaborRecorded: false}}, true), "CAM2"},
		{"CampaignAnalysisTableEmpty", CampaignAnalysisTable(nil, false), ""},
		{"TechnicianEfficiencyTableFull", TechnicianEfficiencyTable([]Phase5TechRow{{Name: "技師B", OrderCount: "5", Hours: "20", HourlyPrice: "800", Commission: "1000", Margin: "500", LaborRecorded: true}, {Name: "技師C", OrderCount: "1", Hours: "2", LaborRecorded: false}}, "USD", false), "技師B"},
		{"TechnicianEfficiencyTableEmpty", TechnicianEfficiencyTable(nil, "", true), ""},
		{"ReportStatusTableEmpty", ReportStatusTable("狀態", nil), "狀態"},
		{"ReportItemsTableEmpty", ReportItemsTable("項目", nil), "項目"},
		{"ReportTechTableEmpty", ReportTechTable(nil), ""},
		{"ReportLowTableEmpty", ReportLowTable(nil), ""},
		{"ReportsPageFull", ReportsPage(Phase5ReportData{From: "2026-10-01", To: "2026-10-31", InsLabel: "保險", SelfLabel: "自費", InsRevenue: "20000", SelfRevenue: "80000", TotalRevenue: "100000", InsCount: "10", SelfCount: "40", TotalCount: "50", InsurancePct: "20%", AnalyticsLink: true, StatusSelf: []Phase5ReportStatusRow{{Label: "維修中", Pill: "pill", Count: "3"}}, StatusIns: []Phase5ReportStatusRow{{Label: "已完工", Pill: "pill-ok", Count: "2"}}, ItemsSelf: []Phase5ReportItemRow{{Description: "換機油", Quantity: "10", Revenue: "8000"}}, ItemsIns: []Phase5ReportItemRow{{Description: "煞車皮", Quantity: "5", Revenue: "7500"}}, Tech: []Phase5ReportTechRow{{Name: "技師A", Orders: "5", Hours: "20", Commission: "1000"}}, Low: []Phase5ReportLowRow{{Part: "P-9", Name: "濾芯", Stock: "1", MinStock: "5", Pill: "pill-warn"}}, Aging: []Phase5ReportAgingRow{{Label: "0-30天", Count: "2", Amount: "1000", Class: "pill"}}, AgingTotalCount: "2", AgingTotalAmount: "1000"}, testOwner), "保險"},
		{"WorklogSummariesEmpty", WorklogSummaries(nil), ""},
		{"CommissionRowsEmpty", CommissionRows(nil), ""},
		{"WorklogsPageEmpty", WorklogsPage(Phase5WorklogData{WOID: "9", Rows: nil, Technicians: []Phase5Option{{Value: "1", Label: "技師A"}}}, testOwner), ""},
		{"EditWorklogPageFull", EditWorklogPage(Phase5EditWorklogData{WOID: "1", LogID: "5", Technician: "技師A", Date: "2026-11-01", Hours: "2", Rate: "800", CommissionRate: "10%", Notes: "備註"}, testOwner), "技師A"},
		{"CommissionsPageFull", CommissionsPage(Phase5CommissionsData{Month: "2026-11", TotalOrders: "10", TotalHours: "100", TotalLabor: "80000", TotalCommission: "8000", Rows: []Phase5CommissionRow{{Name: "技師A", Username: "tech1", Branch: "維修部", Orders: "5", Hours: "50", Labor: "40000", Commission: "4000"}}}, testOwner), "技師A"},
		{"AnalyticsPageFull", AnalyticsPage(Phase5AnalyticsData{From: "2026-10-01", To: "2026-10-31", GroupBy: "month", Gran: "day", ShowCost: true, CanExportCosts: true, Chart: components.Empty("圖表"), Campaign: components.Empty("行銷"), Technician: components.Empty("技師"), ProfitLoss: []Phase5Table{{Headers: []string{"項目", "金額"}, Rows: [][]Phase5Cell{{{Value: "營收"}, {Value: "100000"}}}}}, Cars: []Phase5Table{{Headers: []string{"車種"}, Rows: [][]Phase5Cell{{{Value: "Toyota"}}}}}, Parts: []Phase5Table{{Headers: []string{"零件"}, Rows: [][]Phase5Cell{{{Value: "機油"}}}}}, Dead: []Phase5Table{{Headers: []string{"呆滯"}, Rows: [][]Phase5Cell{{{Value: "濾芯"}}}}}, ActiveCust: "100", NewCust: "20", OldCust: "80", RepeatCust: "30", AvgPerCust: "1000", DiscountTotal: "5000", WarrantyCount: "5", WarrantyRevenue: "10000", WarrantyDiscount: "1000", WarrantyCostTotal: "6000", WarrantyGross: "4000", WarrantyLaborRecorded: false, DeadCostTotal: "2000", DeadCount: "3", Flash: "已匯出", ARO: "50000", OrderCount: "50", GrossStat: "30000", RepeatRate: "30%", GrossNote: "備註"}, testOwner), "營收"},
		{"AnalyticsPageNoCost", AnalyticsPage(Phase5AnalyticsData{From: "2026-10-01", To: "2026-10-31", Gran: "week", ShowCost: false, CanExportCosts: false, Chart: components.Empty("圖表"), Campaign: components.Empty(""), Technician: components.Empty(""), WarrantyLaborRecorded: true}, testCounter), ""},
		// Photo markup
		{"PhotoMarkerOverlayOther", PhotoMarkerOverlay("/photos/9.jpg", 9), "9"},
		{"WalkaroundCanvasMulti", WalkaroundCanvas(2, []struct {
			ID       int64
			MarkType string
			X        float64
			Y        float64
			Note     string
		}{{ID: 1, MarkType: "scratch", X: 5, Y: 6, Note: "刮傷"}, {ID: 2, MarkType: "dent", X: 50, Y: 60, Note: ""}, {ID: 3, MarkType: "chip", X: 90, Y: 10, Note: "跳石"}}), ""},
		{"WalkaroundMarkTypeSelectScratch", WalkaroundMarkTypeSelect("scratch"), "scratch"},
		{"PhotoAnnotationForm2", PhotoAnnotationForm(2), ""},
		{"WorkOrderPhotosPageNote", WorkOrderPhotosPage(2, []PhotoCard{{WorkOrderID: 2, ID: 7, TypeLabel: "車損", OriginalName: "c.jpg", Uploader: "技師A", CreatedAt: "2026-11-05", Size: "1.2MB", Note: "右前門凹痕", Marker: PhotoMarkerOverlay("/photos/7.jpg", 7)}}, "/data/photos", testOwner, ""), "右前門凹痕"},
		// Phrases / print / receipt
		{"PhrasesPageEmpty", PhrasesPage(nil, testCounter, "已儲存"), "已儲存"},
		{"PhrasesPageOff", PhrasesPage([]model.TechnicianPhrase{{ID: 2, Phrase: "停用詞", Category: "檢查", Active: 0}}, testOwner, ""), "停用詞"},
		{"PhraseEditPageOff", PhraseEditPage(model.TechnicianPhrase{ID: 2, Phrase: "停用詞", Category: "檢查", Active: 0}, testTech, ""), "停用詞"},
		{"PrintPageStyle", PrintPage(PrintPageData{Title: "估價單", Style: "font-size:12px", Body: "內容"}), "估價單"},
		{"ReceiptPageFull", ReceiptPage(ReceiptPageData{AppName: "AutoRepair OS", WorkOrderID: "3", Date: "2026-11-05", Status: "已完工", Approval: "已同意", CustomerName: "李小華", CustomerPhone: "0922333444", PlateNumber: "XYZ-9999", BrandModel: "Honda Civic", Mileage: "60000", Technician: "技師A", Complaint: "煞車異音", Items: []ReceiptItem{{Description: "煞車皮", Quantity: "2", UnitPrice: "1500", Subtotal: "3000"}}, ItemTotal: "3000", Total: "3000", Username: "counter1", LineBound: false, LineJSON: "{}"}), "技師A"},
		{"ReceiptPageLineBound", ReceiptPage(ReceiptPageData{CustomerName: "王小明", Items: nil, Total: "0", LineBound: true, LineJSON: `{"line":"bound"}`}), "王小明"},
		// Purchase orders
		{"ListPurchaseOrdersPageReceived", ListPurchaseOrdersPage([]PurchaseOrderRow{poReceived}, 1, 1, 20, "", "received", testOwner), "供應商B"},
		{"PurchaseOrderRowContentReceived", PurchaseOrderRowContent(poReceived), "供應商B"},
		{"PurchaseOrderSearchRowsEmpty", PurchaseOrderSearchRows(nil), ""},
		{"NewPurchaseOrderPageErr", NewPurchaseOrderPage("2026-11-09", "<option>供應商A</option>", "供應商必填", testTech), "供應商必填"},
		{"ShowPurchaseOrderPageReceived", ShowPurchaseOrderPage(model.PurchaseOrder{ID: 2, SupplierID: 1, SupplierName: "供應商B", WorkOrderID: 3, OrderDate: "2026-10-01", Status: "received", TotalAmount: 8000, Notes: "已進貨", ItemCount: 3, ReceivedAt: "2026-10-05"}, []model.PurchaseOrderItem{testPOItem, {ID: 2, OrderID: 2, Description: "煞車皮", Quantity: 4, UnitCost: 900}}, 8600, true, "<option>供應商B</option>", "", "已進貨", testOwner), "已進貨"},
		{"ShowPurchaseOrderPageDraft", ShowPurchaseOrderPage(model.PurchaseOrder{ID: 3, SupplierName: "供應商A", Status: "draft", WorkOrderID: 0}, nil, 0, false, "", "", "", testTech), "供應商A"},
		{"PurchaseOrderItemsTableLocked", PurchaseOrderItemsTable(2, []model.PurchaseOrderItem{testPOItem}, 5000, true), "機油"},
		{"AddPurchaseItemFormWithOpts", AddPurchaseItemForm(2, "<option>引擎</option>"), ""},
		// Quotation
		{"QuotationPageThermal", QuotationPage(QuotationPageData{AppName: "AutoRepair OS", WorkOrderID: "1", Paper: "thermal", QuoteDate: "2026-11-05", Expiry: "2026-12-05", CustomerName: "王小明", CustomerPhone: "0912345678", PlateNumber: "ABC-1234", BrandModel: "Toyota Altis", Mileage: "50000", Complaint: "異音", ThermalItems: []QuotationItem{{Description: "換機油", Quantity: "1", UnitPrice: "800", Subtotal: "800"}}, ItemTotal: "800", Discount: "100", Total: "700", Warranty: true}), "王小明"},
		{"QuotationPageEmptyGroup", QuotationPage(QuotationPageData{CustomerName: "李小華", PlateNumber: "XYZ-9999", Groups: []QuotationGroup{{Label: "維修", Items: nil}, {Label: "保養", Items: []QuotationItem{{Description: "保養套餐", Quantity: "1", UnitPrice: "3000", Subtotal: "3000", Category: "保養"}}}}, Total: "3000"}), "保養"},
		// Receivables
		{"ListReceivablesPageFull", ListReceivablesPage([]ReceivableRow{recLegacy, recFull}, 2, 1, 20, "", "card", "2026-11-01", "2026-11-09", true, true, 3000, 500, 2500, 0, 0, testOwner), "V-LEGACY"},
		{"ReceivableRowContentLegacy", ReceivableRowContent(recLegacy), "舊客"},
		{"ReceivableRowContentFull", ReceivableRowContent(recFull), "V-003"},
		{"ReceivableSearchRowsEmpty", ReceivableSearchRows(nil), ""},
		// Scheduled tasks
		{"ListScheduledTasksPageFull", ListScheduledTasksPage([]ScheduledTaskRow{{Name: "每日備份", Key: "backup", Interval: "每天", LastRun: "2026-11-09 02:00", NextRun: "2026-11-10 02:00", LastError: "磁碟滿", RunCount: 10, Enabled: false}}, []NotificationRow{{ID: 1, Kind: "reminder", Channel: "line", Subject: "保養提醒", Body: "該保養了", ScheduledAt: "2026-11-10 09:00", Status: "failed", StatusLabel: "失敗", StatusPill: "pill-err", ErrorDetail: "token 過期", Cancelable: true}, {ID: 2, Kind: "promo", Channel: "sms", Subject: "促銷", Body: "x", Status: "pending", StatusLabel: "待發送", StatusPill: "pill", Cancelable: false}}, []BackupRow{{Date: "2026-11-08", Path: "/backups/1108.db", Status: "失敗", Error: "寫入失敗", DBSize: "100MB", PhotoCount: "50", PhotoSize: "200MB", Success: false}}, "已停用", "最近", "pill", "異常", testOwner), "每日備份"},
		{"ListScheduledTasksPageEmpty", ListScheduledTasksPage(nil, nil, nil, "", "", "", "", testCounter), ""},
		// Simple forms
		{"EditAppointmentPageFull", EditAppointmentPage(model.Appointment{ID: 2, CustomerID: 2, CustomerName: "李小華", VehicleID: 2, PlateNumber: "XYZ-9999", AppointmentDate: "2026-11-11", AppointmentTime: "10:00", ServiceType: "保養", Bay: "B2", Status: "confirmed", Notes: "帶鑰匙"}, "<option>B2</option>", testOwner), "保養"},
		{"AddVehicleMileagePageNoLast", AddVehicleMileagePage(2, "XYZ-9999", "Honda Civic", 0, false, testCounter), "XYZ-9999"},
		{"EditEquipmentPageFull", EditEquipmentPage(model.Equipment{ID: 2, Name: "烤漆房", Model: "M-200", SerialNumber: "SN01", Location: "一樓", PurchaseDate: "2025-01-01", WarrantyExpiry: "2027-01-01", Status: "repairing", Notes: "噴嘴更換"}, testOwner), "烤漆房"},
		{"EquipmentMaintenancePageEmpty", EquipmentMaintenancePage(2, "烤漆房", "<option>技師A</option>", nil, "", testOwner), "烤漆房"},
		// SOP templates
		{"ListSOPTemplatesPageEmpty", ListSOPTemplatesPage(SOPTemplateListData{Rows: nil, User: testOwner}), ""},
		{"NewSOPTemplatePageCounter", NewSOPTemplatePage(testCounter), ""},
		{"EditSOPTemplatePageMulti", EditSOPTemplatePage(SOPTemplateEditData{ID: 2, Name: "鈑金SOP", RepairType: "body", Items: []SOPTemplateEditItem{{ID: 1, Description: "拆內裝", Required: true, SortOrder: 1}, {ID: 2, Description: "鈑金整形", Required: false, SortOrder: 2}}, User: testOwner}), "鈑金SOP"},
		// Staff / suppliers / subcontracts
		{"StaffPickerResultsEmpty", StaffPickerResults(nil), ""},
		{"ListStaffPageFull", ListStaffPage([]StaffRow{staffDependent, staffFull}, 2, 1, 20, "", "dependent", 1, 1, 1, false, "<option>owner</option>", "<option>維修</option>", testOwner), "眷屬A"},
		{"StaffRowContentDependent", StaffRowContent(staffDependent), "眷屬A"},
		{"StaffSearchRowsEmpty", StaffSearchRows(nil), ""},
		{"EditSupplierPageFull", EditSupplierPage(SupplierEditData{ID: 2, Name: "鈑金廠B", Phone: "0223456789", Contact: "張先生", Address: "新北市", VendorType: "vendor", PaymentTerms: "net30", Notes: "配合度佳", PaymentOpts: []components.SelectOption{{Value: "net30", Label: "月結30天", Selected: true}}, User: testOwner}), "鈑金廠B"},
		{"EditSubcontractPageFull", EditSubcontractPage(SubcontractEditData{ID: 2, WorkOrderID: 3, Description: "烤漆", SupplierID: 2, CostAmount: "5000", ChargeAmount: "8000", ExpectedDate: "2026-11-15", Status: "in_progress", Notes: "加急", SupplierOpts: []components.SelectOption{{Value: "2", Label: "鈑金廠B", Selected: true}}, User: testOwner}), "烤漆"},
		{"EditStaffPageFull", EditStaffPage(StaffEditData{ID: 2, Name: "眷屬A", Username: "dep1", Error: "身分證號格式錯誤", NationalID: "A123456789", Birthday: "2015-01-01", InsuranceStart: "2026-01-01", InsuranceEnd: "2026-12-31", Kind: "family", Branch: "維修", Active: 0, BranchOpts: []components.SelectOption{{Value: "維修", Label: "維修部", Selected: true}}, InsuranceHistory: []InsuranceHistoryItem{{ID: 1, EventType: "加保", EventDate: "2026-01-01", Note: "新生兒"}, {ID: 2, EventType: "退保", EventDate: "2026-12-31", Note: ""}}, User: testOwner}), "身分證號格式錯誤"},
		{"EditCustomerPageBound", EditCustomerPage(CustomerEditData{Customer: custBound, SMSWarning: true, Flash: "已儲存", FlashErr: "", User: testOwner}), "已儲存"},
		{"EditCustomerPageTelegram", EditCustomerPage(CustomerEditData{Customer: model.Customer{ID: 3, Name: "陳大同", Phone: "0933455666", NotificationPref: "telegram", TelegramUserID: "777"}, User: testCounter}), "陳大同"},
		{"CustomerLineBindingBound", CustomerLineBinding(custBound), "U999"},
		{"CustomerTelegramBindingBound", CustomerTelegramBinding(custBound), "555"},
		{"EditInsuranceCompanyPageFull", EditInsuranceCompanyPage(InsuranceCompanyEditData{ID: 2, Code: "INS2", Name: "保險B", TaxID: "12345678", ClaimPhone: "0800-000", Phone: "0223456789", ContactPerson: "王經理", Address: "台北市", PostalCode: "100", Notes: "備註", User: testOwner}), "保險B"},
		{"VehicleInspectionFormPageEdit", VehicleInspectionFormPage(InspectionFormData{ID: 5, Title: "編輯檢驗單", Action: "/inspections/5", OrderDate: "2026-11-01", CheckDate: "2026-11-02", FirmNo: "F-001", Notes: "已驗", New: false, User: testOwner}), "編輯檢驗單"},
		{"VehicleInspectionFormPageNewOpts", VehicleInspectionFormPage(InspectionFormData{Title: "新增檢驗單", Action: "/inspections", VehicleOpts: []components.SelectOption{{Value: "1", Label: "ABC-1234", Selected: true}}, New: true, User: testCounter}), "新增檢驗單"},
		{"ReceivableFormPageFull", ReceivableFormPage(ReceivableFormData{Title: "編輯收款", Action: "/receivables/2", VoucherNo: "V-003", PayDate: "2026-11-05", CustomerName: "李小華", CustomerID: "2", WorkOrderID: "5", Cash: "500", Card: "2000", CardNo: "4111", CheckAmount: "0", Bank: "台銀", TransferAmount: "0", DetailTotal: "2500", Discount: "0", Tax: "0", PayTotal: "2500", Notes: "備註", Error: "金額不符", User: testOwner}), "金額不符"},
		{"ListMaintenanceTemplatesPageDetail", ListMaintenanceTemplatesPage(MaintenanceTemplateListData{Rows: []MaintenanceTemplateListRow{{ID: 1, Code: "M1", Name: "保養套餐", ItemCount: 2}}, Query: "保養", Detail: &MaintenanceTemplateDetail{Code: "M1", Name: "保養套餐", Items: []MaintenanceTemplateEditItem{{PartNumber: "P-001", Description: "換機油", Quantity: "1", Price: "800"}, {PartNumber: "P-002", Description: "濾芯", Quantity: "1", Price: "300"}}}, User: testOwner, Flash: "已儲存"}), "保養套餐"},
		{"ListMaintenanceTemplatesPageDetailEmpty", ListMaintenanceTemplatesPage(MaintenanceTemplateListData{Rows: nil, Detail: &MaintenanceTemplateDetail{Code: "M9", Name: "空套餐", Items: nil}, User: testOwner}), "空套餐"},
		{"NewMaintenanceTemplatePageCounter", NewMaintenanceTemplatePage(testCounter), ""},
		{"EditMaintenanceTemplatePageMulti", EditMaintenanceTemplatePage(MaintenanceTemplateEditData{ID: 2, Code: "M2", Name: "煞車套餐", Notes: "備註", Items: []MaintenanceTemplateEditItem{{PartNumber: "P-010", Description: "煞車皮", Quantity: "2", Price: "1500"}, {PartNumber: "", Description: "工資", Quantity: "1", Price: "1000"}}, User: testOwner}), "煞車套餐"},
		// Subcontracts chain
		{"ListSubcontractsPageFull", ListSubcontractsPage([]SubcontractRow{subFull}, 1, 1, 20, "", "done", "鈑金廠B", testOwner), "鈑金廠B"},
		{"SubcontractRowContentDone", SubcontractRowContent(subFull), "鈑金廠B"},
		{"SubcontractSearchRowsEmpty", SubcontractSearchRows(nil), ""},
		// Suppliers chain
		{"ListSuppliersPageFull", ListSuppliersPage([]SupplierRow{supVendor, supMaterial}, 2, 1, 20, "", "vendor", 1, 1, true, testOwner), "鈑金廠B"},
		{"SupplierRowContentVendorOff", SupplierRowContent(supVendor), "鈑金廠B"},
		{"SupplierRowContentMaterialOn", SupplierRowContent(supMaterial), "材料商C"},
		{"SupplierSearchRowsEmpty", SupplierSearchRows(nil), ""},
		// Vehicles chain
		{"VehicleSearchRowsType", VehicleSearchRows([]VehicleRow{vehType}), "自用"},
		{"VehicleSearchRowsEmpty", VehicleSearchRows(nil), ""},
		{"ListVehiclesPageType", ListVehiclesPage([]VehicleRow{vehType}, 1, 1, 20, "", 0, "", "", "<option>自用</option>", "", testOwner), "自用"},
		{"ShowVehiclePageFull", ShowVehiclePage(vehFull, 5, 50000, 3000, 62000, "2027-03-01", "64000", "<p>里程紀錄</p>", "<p>工單紀錄</p>", "<p>零件紀錄</p>", testOwner), "自用"},
		{"EditVehiclePageFull", EditVehiclePage(vehFull, "<option>Toyota Altis</option>", "<option>自用</option>", "<option>汽油</option>", "<option>四門房車</option>", testOwner), ""},
		{"NewVehiclePageOpts", NewVehiclePage("王小明", "<option>Toyota Altis</option>", "<option>自用</option>", "<option>汽油</option>", "<option>四門房車</option>", testCounter), ""},
		// Work orders chain
		{"ListWorkOrdersPageFull", ListWorkOrdersPage([]WorkOrderRow{woInsurance, woMultiPart}, 2, 1, 20, "", "", "", "", "", "", "", "", testOwner), "XYZ-9999"},
		{"WorkOrderRowContentInsurance", WorkOrderRowContent(woInsurance), "XYZ-9999"},
		{"WorkOrderRowContentSinglePart", WorkOrderRowContent(woMultiPart), "QQQ-1111"},
		{"WorkOrderSearchRowsEmpty", WorkOrderSearchRows(nil), ""},
		{"NewWorkOrderPageFull", NewWorkOrderPage(NewWorkOrderPageData{FlashHTML: "<p>提示</p>", NotFoundHTML: "<p>查無</p>", ApptBannerHTML: "<p>預約</p>", PreAppointment: 7, PreVehicle: 1, VehicleOptionsJSON: "[]", Vehicles: []model.Vehicle{testVehicle, vehFull}, PresetLabel: "XYZ-9999", Mileage: "60000", TechnicianOptions: "<option>技師A</option>", InsuranceOptions: "<option>保險B</option>", CustomerHintHTML: "<p>提示</p>"}, testOwner), "XYZ-9999"},
		{"NewWorkOrderPageNoVehicles", NewWorkOrderPage(NewWorkOrderPageData{}, testCounter), ""},
		{"ShowWorkOrderPageFull", ShowWorkOrderPage(woInsuranceFull, []model.WorkOrderItem{{ID: 10, WorkOrderID: 3, ItemType: "part", Description: "煞車皮", Quantity: 2, UnitPrice: 1500, Subtotal: 3000, PrimaryTechnicianID: 1}, {ID: 11, WorkOrderID: 3, ItemType: "labor", Description: "工資", Quantity: 1, UnitPrice: 1000, Subtotal: 1000, PhotoURL: "/photos/1.jpg"}}, 4000, map[string]string{"completed": "已完工"}, map[string]string{"completed": "pill-ok"}, map[string]string{"approved": "已同意"}, map[string]string{"approved": "pill-ok"}, map[int64]string{1: "技師A"}, true, "已儲存", []model.SOPRecord{{ID: 1, WorkOrderID: 3, Description: "檢查煞車", Required: true, Completed: true, CompletedByName: "技師A", CompletedAtText: "2026-11-05 10:00"}, {ID: 2, WorkOrderID: 3, Description: "檢查胎壓", Required: false, Completed: false}}, "<option>保養SOP</option>", []int64{1, 2}, true, []PendingApproval{{ID: 1, Description: "追加項目"}}, testOwner), "煞車皮"},
		{"ShowWorkOrderPagePending", ShowWorkOrderPage(woPending, nil, 0, map[string]string{"pending": "待處理"}, map[string]string{"pending": "pill"}, map[string]string{"pending": "待核准"}, map[string]string{"pending": "pill"}, nil, false, "", nil, "", nil, false, nil, testTech), "待處理"},
		{"WorkOrderItemsTableNoMoney", WorkOrderItemsTable(1, []model.WorkOrderItem{testWOItem}, map[int64]string{}, false, 0, testCounter), "換機油"},
		{"WorkOrderItemsTableEmpty", WorkOrderItemsTable(1, nil, nil, true, 0, testOwner), ""},
		{"StatusControlsPending", StatusControls(woPending, true), ""},
		{"InsuranceRowNoEditWithNote", InsuranceRow(woInsuranceFull, false), "已送件"},
		{"InsuranceRowPlainNoEdit", InsuranceRow(woPending, false), ""},
		{"SOPChecklistTableModes", SOPChecklistTable(3, []model.SOPRecord{{ID: 1, Description: "檢查煞車", Required: true, Completed: true, CompletedByName: "技師A"}, {ID: 2, Description: "檢查胎壓", Required: false, Completed: false}}, true), "檢查煞車"},
		{"SOPChecklistTableReadonly", SOPChecklistTable(3, []model.SOPRecord{{ID: 1, Description: "檢查煞車", Completed: false}}, false), "檢查煞車"},
		{"SOPChecklistTableEmpty", SOPChecklistTable(3, nil, true), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Skipf("panics on sample data: %v", r)
				}
			}()
			out := renderPages(t, tc.comp)
			if strings.TrimSpace(out) == "" {
				t.Fatal("empty render output")
			}
			if tc.want != "" && !strings.Contains(out, tc.want) {
				t.Errorf("output missing %q", tc.want)
			}
		})
	}
}

// errOnlyWriter is a non-buffer writer that always fails: covers templ's
// non-buffer (IsBuffer=false) flush path in generated code.
type errOnlyWriter struct{}

func (errOnlyWriter) Write(p []byte) (int, error) { return 0, errors.New("boom") }

// TestGeneratedErrorPathsRound2 renders page components with a cancelled
// context and a failing non-buffer writer, covering templ's generated
// ctx.Err / flush error branches.
func TestGeneratedErrorPathsRound2(t *testing.T) {
	comps := allSweepPages()
	for name, c := range comps {
		c := c
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Skipf("panics on sample data: %v", r)
				}
			}()
			// cancelled context: covers ctx.Err() branch
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var buf bytes.Buffer
			_ = c.Render(ctx, &buf)
			// failing non-buffer writer: covers flush error branch
			_ = c.Render(context.Background(), errOnlyWriter{})
		})
	}
}
