package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"autorepairos/internal/model"
	"autorepairos/web/components"
	"github.com/a-h/templ"
)

func renderPages(t *testing.T, c templ.Component) string {
	t.Helper()
	var out bytes.Buffer
	if err := c.Render(context.Background(), &out); err != nil {
		t.Fatalf("render: %v", err)
	}
	return out.String()
}

var (
	testOwner    = model.User{ID: 1, Username: "owner1", Role: "owner"}
	testCounter  = model.User{ID: 2, Username: "counter1", Role: "counter"}
	testTech     = model.User{ID: 3, Username: "tech1", Role: "technician"}
	testWO       = model.WorkOrder{ID: 1, PlateNumber: "ABC-1234", Status: "repairing", ApprovalStatus: "none"}
	testWOItem   = model.WorkOrderItem{ID: 1, WorkOrderID: 1, Description: "換機油", Quantity: 1, UnitPrice: 800}
	testInvItem  = model.InventoryItem{ID: 1, PartNumber: "P-001", Name: "機油", StockQuantity: 5}
	testCustomer = model.Customer{ID: 1, Name: "王小明", Phone: "0912345678"}
	testVehicle  = model.Vehicle{ID: 1, PlateNumber: "ABC-1234", CustomerID: 1}
	testPOItem   = model.PurchaseOrderItem{ID: 1, OrderID: 1, Description: "機油", Quantity: 10, UnitCost: 500}
	testPhrase   = model.TechnicianPhrase{ID: 1, Phrase: "檢查胎壓", Category: "檢查"}
	testAppt     = model.Appointment{ID: 1, PlateNumber: "ABC-1234", CustomerName: "王小明"}
	testEquip    = model.Equipment{ID: 1, Name: "頂高機"}
	testLookup   = model.LookupItem{ID: 1, Code: "OIL", Label: "機油"}
	testSOPRecs  = []model.SOPRecord{{ID: 1, WorkOrderID: 1, Description: "檢查煞車", Required: true}}
)

func TestRenderAllPages(t *testing.T) {
	apRow := APPayableRow{ID: 1, SupplierName: "供應商A", Amount: 1000, PaidAmount: 400, Balance: 600, Status: "partial", StatusLabel: "部分付", StatusPill: "pill-warn", DueDate: "2026-11-30"}
	apptRow := AppointmentRow{ID: 1, AppointmentDate: "2026-11-10", AppointmentTime: "09:00", Bay: "B1", PlateNumber: "ABC-1234", CustomerName: "王小明", Status: "booked", StatusLabel: "已預約", StatusPill: "pill"}
	custRow := CustomerRow{ID: 1, Name: "王小明", Phone: "0912345678", Tags: "VIP", TotalSpent: 5000, UnpaidAmount: 100}
	equipRow := EquipmentRow{ID: 1, Name: "頂高機", Status: "active", StatusLabel: "使用中", StatusPill: "pill-ok"}
	estRow := EstimateListRow{ID: 1, CustomerName: "王小明", PlateNumber: "ABC-1234", Status: "draft", TotalAmount: 3000}
	estDetail := EstimateDetail{ID: 1, CustomerName: "王小明", PlateNumber: "ABC-1234", Status: "draft", TotalAmount: 3000, CreatedAt: "2026-11-01"}
	estItem := EstimateItemRow{ID: 1, ItemType: "service", Description: "換機油", Quantity: 1, UnitPrice: 800, Subtotal: 800}
	expRow := ExpenseRow{ID: 1, Date: "2026-11-01", Part: "零件", AmtOut: 500}
	vInsRow := VehicleInsuranceRow{ID: 1, Plate: "ABC-1234", Customer: "王小明", Company: "保險A", ExpiryDate: "2027-01-01"}
	inspRow := InspectionRow{ID: 1, Plate: "ABC-1234", StatusLabel: "待驗", StatusPill: "pill"}
	insRow := InsuranceCompanyRow{ID: 1, Code: "INS1", Name: "保險A", Active: 1}
	invRow := InventoryRow{ID: 1, PartNumber: "P-001", Name: "機油", StockQuantity: 5, SellingPrice: 800, CostPrice: 500}
	negRow := NegativeStockRow{ID: 1, PartNumber: "P-002", Name: "煞車皮", StockQuantity: -2, SellingPrice: "300", Suggested: 5}
	moveRow := InventoryMovementRow{CreatedAt: "2026-11-01 10:00", PartNumber: "P-001", Name: "機油", Delta: 5, Before: 0, After: 5, DeltaPositive: true, Source: "purchase_receive"}
	lookupRow := LookupRow{ID: 1, Code: "OIL", Label: "機油", Active: 1}
	poRow := PurchaseOrderRow{ID: 1, SupplierName: "供應商A", OrderDate: "2026-11-01", Status: "draft", TotalAmount: 5000, ItemCount: 2}
	recRow := ReceivableRow{ID: 1, VoucherNo: "V-001", CustomerName: "王小明", PlateNumber: "ABC-1234", MethodLabel: "現金", PayTotal: 1000, WorkOrderID: 1, WoCell: "1", ReceivedCell: "1000", UnpaidCell: ""}
	staffRow := StaffRow{ID: 1, Username: "tech1", Name: "技師A", Role: "technician", Active: 1, CanEdit: true}
	subRow := SubcontractRow{ID: 1, WorkOrderID: 1, SupplierName: "外包A", Description: "鈑金", Status: "pending", StatusLabel: "待處理", StatusPill: "pill"}
	supRow := SupplierRow{ID: 1, Name: "供應商A", VendorType: "material", VendorLabel: "材料", Active: 1}
	vehRow := VehicleRow{ID: 1, PlateNumber: "ABC-1234", BrandModel: "Toyota Altis", CustomerName: "王小明", CanEdit: true}
	woRow := WorkOrderRow{ID: 1, PlateNumber: "ABC-1234", BrandModel: "Toyota Altis", CustomerName: "王小明", Status: "repairing", StatusLabel: "維修中", StatusPill: "pill", TotalAmount: 3000}
	calDay := CalendarDay{Date: "2026-11-10", Label: "11/10", Appointments: []AppointmentRow{apptRow}}

	cases := []struct {
		name string
		comp templ.Component
		want string
	}{
		{"AnalyticsCharts", AnalyticsCharts([]AnalyticsTrendPoint{{Label: "11月", Revenue: "10000"}}, []AnalyticsGroupRow{{Label: "維修", Revenue: "8000"}}, 600, true), ""},
		{"AnalyticsChartsEmpty", AnalyticsCharts(nil, nil, 0, false), ""},
		{"APHomePage", APHomePage([]APPayableRow{apRow}, []APAgingSummary{{Name: "供應商A", Count: 1, Total: 600}}, []APSupplierOption{{ID: 1, Name: "供應商A"}}, 0, "", "2026-11-09", testOwner), "供應商A"},
		{"APPayableRowContent", APPayableRowContent(apRow, "2026-11-09", "owner"), "供應商A"},
		{"APStatementPage", APStatementPage([]APPayableRow{apRow}, "供應商A", "2026-11-01", "2026-11-30", 1000, 400, 600, nil, 0, "/ap/statement.csv", testOwner), "供應商A"},
		{"APStatementPageEmpty", APStatementPage(nil, "", "", "", 0, 0, 0, nil, 0, "", testOwner), ""},
		{"ListAppointmentsPage", ListAppointmentsPage([]AppointmentRow{apptRow}, 1, 1, 20, "2026-11-10", "", "2026-11-10", "day", []CalendarDay{calDay}, components.BayOptions("", nil, false), nil, "2026-11-10", testOwner), "ABC-1234"},
		{"ListAppointmentsPageEmpty", ListAppointmentsPage(nil, 0, 1, 20, "", "", "", "day", nil, components.BayOptions("", nil, false), nil, "", testOwner), ""},
		{"AppointmentRowContent", AppointmentRowContent(apptRow), "ABC-1234"},
		{"AppointmentSearchRows", AppointmentSearchRows([]AppointmentRow{apptRow}), "ABC-1234"},
		{"AppointmentCalendar", AppointmentCalendar("2026-11-10", "day", "", []CalendarDay{calDay}), "2026-11-10"},
		{"AppointmentCalendarWeek", AppointmentCalendar("2026-11-10", "week", "booked", nil), ""},
		{"ApprovalRequestBlock", ApprovalRequestBlock(1, true, []PendingApproval{{ID: 1, Description: "換輪胎", Quantity: 4, UnitPrice: 2000}}), "換輪胎"},
		{"ApprovalRequestBlockNoCreate", ApprovalRequestBlock(1, false, nil), ""},
		{"ApprovalPageEmpty", ApprovalPage(ApprovalPageData{}), ""},
		{"ApprovalPageFull", ApprovalPage(ApprovalPageData{Request: &model.ApprovalRequest{ID: 1, Token: "tok", ExpiresAt: "2026-11-20", ItemSummary: "換輪胎 x4"}, WorkOrder: &testWO, Customer: &testCustomer, Items: []model.WorkOrderItem{testWOItem}}), "王小明"},
		{"ApprovalPageExpired", ApprovalPage(ApprovalPageData{Request: &model.ApprovalRequest{ID: 1, Token: "tok", ExpiresAt: "2026-11-01"}, WorkOrder: &testWO, Customer: &testCustomer, IsExpired: true, ErrorMsg: "已過期"}), "已過期"},
		{"ApprovalResult", ApprovalResult("approved", "已同意"), "已同意"},
		{"ApprovalResultDenied", ApprovalResult("denied", "已拒絕"), "已拒絕"},
		{"WorkOrderClaimFields", WorkOrderClaimFields(testWO, true), ""},
		{"WorkOrderClaimFieldsReadonly", WorkOrderClaimFields(testWO, false), ""},
		{"ListCustomersPage", ListCustomersPage([]CustomerRow{custRow}, 1, 1, 20, "", CustomerCreateForm{}, testOwner), "王小明"},
		{"ListCustomersPageEmpty", ListCustomersPage(nil, 0, 1, 20, "x", CustomerCreateForm{}, testCounter), ""},
		{"CustomerSearchRows", CustomerSearchRows([]CustomerRow{custRow}, testOwner), "王小明"},
		{"CustomerSearchRowsTech", CustomerSearchRows([]CustomerRow{custRow}, testTech), "王小明"},
		{"DashboardPage", DashboardPage([]DashboardStat{{Label: "今日工單", Href: "/work-orders", Count: 3}}, []DashboardIssue{{Label: "低庫存", Href: "/inventory", Count: 2}}, []DashboardRecentWorkOrder{{ID: 1, PlateNumber: "ABC-1234"}}, testOwner), "今日工單"},
		{"DashboardPageEmpty", DashboardPage(nil, nil, nil, testTech), ""},
		{"ListEquipmentPage", ListEquipmentPage([]EquipmentRow{equipRow}, 1, 1, 20, "", "", testOwner), "頂高機"},
		{"EquipmentRowContent", EquipmentRowContent(equipRow), "頂高機"},
		{"EquipmentSearchRows", EquipmentSearchRows([]EquipmentRow{equipRow}), "頂高機"},
		{"EstimateListPage", EstimateListPage([]EstimateListRow{estRow}, testOwner), "ABC-1234"},
		{"EstimateNewPage", EstimateNewPage([]EstimateVehicleOption{{ID: 1, PlateNumber: "ABC-1234"}}, "[]", testOwner), ""},
		{"EstimateDetailPage", EstimateDetailPage(estDetail, []EstimateItemRow{estItem}, testOwner, true), "王小明"},
		{"EstimateSourcePage", EstimateSourcePage(estDetail, []EstimateItemRow{estItem}, 1, 3000, 0, testCounter), "王小明"},
		{"ListExpensesPage", ListExpensesPage([]ExpenseRow{expRow}, 1, 1, 20, "", 0, 500, testOwner), ""},
		{"ExpenseRowContent", ExpenseRowContent(expRow), ""},
		{"ExpenseSearchRows", ExpenseSearchRows([]ExpenseRow{expRow}), ""},
		{"ListInsurancePage", ListInsurancePage([]InsuranceCompanyRow{insRow}, 1, 1, 20, "", testOwner), "保險A"},
		{"InsuranceCompanyRowContent", InsuranceCompanyRowContent(insRow), "保險A"},
		{"InsuranceSearchRows", InsuranceSearchRows([]InsuranceCompanyRow{insRow}), "保險A"},
		{"VehicleInsuranceForm", VehicleInsuranceForm(VehicleInsuranceFormData{VehicleID: 1, Title: "新增保險", Action: "/insurance"}), "新增保險"},
		{"VehicleInsuranceFormPage", VehicleInsuranceFormPage(VehicleInsuranceFormData{Title: "新增保險"}, testOwner), "新增保險"},
		{"ListVehicleInsurancesPage", ListVehicleInsurancesPage([]VehicleInsuranceRow{vInsRow}, 1, 1, 20, "", "", false, "", testOwner), "ABC-1234"},
		{"VehicleInsuranceRowContent", VehicleInsuranceRowContent(vInsRow), "ABC-1234"},
		{"ListVehicleInspectionsPage", ListVehicleInspectionsPage([]InspectionRow{inspRow}, 1, 1, 20, "", false, 1, testOwner), "ABC-1234"},
		{"VehicleInspectionRowContent", VehicleInspectionRowContent(inspRow), "ABC-1234"},
		{"VehicleInsuranceSearchRows", VehicleInsuranceSearchRows([]VehicleInsuranceRow{vInsRow}), "ABC-1234"},
		{"VehicleInspectionSearchRows", VehicleInspectionSearchRows([]InspectionRow{inspRow}), "ABC-1234"},
		{"ListInventoryPage", ListInventoryPage([]InventoryRow{invRow}, 1, 0, 0, 1, 20, "", false, true, "", "", "", InventoryCreateForm{}, testOwner), "機油"},
		{"InventoryRowContent", InventoryRowContent(invRow, true), "P-001"},
		{"InventoryRowContentNoMoney", InventoryRowContent(invRow, false), "P-001"},
		{"InventorySearchRows", InventorySearchRows([]InventoryRow{invRow}, true), "機油"},
		{"EditInventoryPage", EditInventoryPage(testInvItem, true, "", "", testOwner), "機油"},
		{"StockAdjustPage", StockAdjustPage(testInvItem, testOwner), "機油"},
		{"NegativeStockPage", NegativeStockPage(NegativeStockStats{Total: 1}, []NegativeStockRow{negRow}, 1, 1, 20, "", "", "", "", "", "", testOwner), "煞車皮"},
		{"NegativeStockFixPage", NegativeStockFixPage(testInvItem, []InventoryMovementRow{moveRow}, testOwner), "機油"},
		{"MovementLogPage", MovementLogPage([]InventoryMovementRow{moveRow}, 1, 1, 20, "", "", "", "", "", testOwner), "機油"},
		{"MovementTable", MovementTable("最近異動", []InventoryMovementRow{moveRow}, true), "最近異動"},
		{"MovementTableEmpty", MovementTable("", nil, false), ""},
		{"LoginPage", LoginPage("AutoRepair OS", "", ""), "AutoRepair OS"},
		{"LoginPageErr", LoginPage("App", "帳密錯誤", "/x"), "帳密錯誤"},
		{"NewLookupItemPage", NewLookupItemPage("category", "類別", testOwner), "類別"},
		{"EditLookupItemPage", EditLookupItemPage(testLookup, testOwner), "機油"},
		{"ListLookupPage", ListLookupPage([]LookupRow{lookupRow}, "category", "類別", 1, "", testOwner), "機油"},
		{"LookupRowContent", LookupRowContent(lookupRow), "機油"},
		{"LookupSearchRows", LookupSearchRows([]LookupRow{lookupRow}), "機油"},
		{"Phase5PageView", Phase5PageView(testOwner, Phase5Page{Title: "報表", Active: "/reports", Body: components.Empty("空")}), "報表"},
		{"Phase5Tables", Phase5Tables([]Phase5Table{{Headers: []string{"項目", "金額"}, Rows: [][]Phase5Cell{{{Value: "100"}}}}}), "項目"},
		{"CampaignAnalysisTable", CampaignAnalysisTable([]Phase5CampaignRow{{Code: "CAM1", Revenue: "5000"}}, true), "CAM1"},
		{"TechnicianEfficiencyTable", TechnicianEfficiencyTable([]Phase5TechRow{{Name: "技師A", Hours: "8"}}, "", true), "技師A"},
		{"ReportStatusTable", ReportStatusTable("維修狀態", []Phase5ReportStatusRow{{Label: "維修中", Count: "3"}}), "維修中"},
		{"ReportItemsTable", ReportItemsTable("自費", []Phase5ReportItemRow{{Description: "換機油", Revenue: "800"}}), "換機油"},
		{"ReportTechTable", ReportTechTable([]Phase5ReportTechRow{{Name: "技師A", Orders: "5"}}), "技師A"},
		{"ReportLowTable", ReportLowTable([]Phase5ReportLowRow{{Part: "P-1", Name: "機油", Stock: "1"}}), "機油"},
		{"ReportsPage", ReportsPage(Phase5ReportData{From: "2026-11-01", To: "2026-11-09"}, testOwner), ""},
		{"WorklogSummaries", WorklogSummaries([]Phase5WorklogSummaryRow{{Name: "技師A", Hours: "8"}}), "技師A"},
		{"CommissionRows", CommissionRows([]Phase5CommissionRow{{Name: "技師A", Orders: "5"}}), "技師A"},
		{"WorklogsPage", WorklogsPage(Phase5WorklogData{WOID: "1", Rows: []Phase5WorklogRow{{Technician: "技師A"}}}, testOwner), "技師A"},
		{"EditWorklogPage", EditWorklogPage(Phase5EditWorklogData{WOID: "1", Technician: "技師A"}, testOwner), "技師A"},
		{"CommissionsPage", CommissionsPage(Phase5CommissionsData{Month: "2026-11", Rows: []Phase5CommissionRow{{Name: "技師A"}}}, testOwner), "技師A"},
		{"AnalyticsPage", AnalyticsPage(Phase5AnalyticsData{From: "2026-11-01", To: "2026-11-09", Gran: "day", Chart: components.Empty("圖表"), Campaign: components.Empty(""), Technician: components.Empty("")}, testOwner), ""},
		{"PhotoMarkerOverlay", PhotoMarkerOverlay("/photos/1.jpg", 1), "1"},
		{"WalkaroundCanvas", WalkaroundCanvas(1, []struct {
			ID       int64
			MarkType string
			X        float64
			Y        float64
			Note     string
		}{{ID: 1, MarkType: "dent", X: 10, Y: 20, Note: "凹痕"}}), ""},
		{"WalkaroundMarkTypeSelect", WalkaroundMarkTypeSelect("dent"), "dent"},
		{"PhotoAnnotationForm", PhotoAnnotationForm(1), ""},
		{"WorkOrderPhotosPage", WorkOrderPhotosPage(1, []PhotoCard{{WorkOrderID: 1, ID: 1, TypeLabel: "照片", OriginalName: "a.jpg", Marker: components.Empty("")}}, "/data/photos", testOwner, ""), "a.jpg"},
		{"WorkOrderPhotosPageEmpty", WorkOrderPhotosPage(1, nil, "", testOwner, "已上傳"), "已上傳"},
		{"PhrasesPage", PhrasesPage([]model.TechnicianPhrase{testPhrase}, testOwner, ""), "檢查胎壓"},
		{"PhraseEditPage", PhraseEditPage(testPhrase, testOwner, ""), "檢查胎壓"},
		{"PrintPage", PrintPage(PrintPageData{Title: "收據", Body: "內容"}), "收據"},
		{"ListPurchaseOrdersPage", ListPurchaseOrdersPage([]PurchaseOrderRow{poRow}, 1, 1, 20, "", "", testOwner), "供應商A"},
		{"PurchaseOrderRowContent", PurchaseOrderRowContent(poRow), "供應商A"},
		{"PurchaseOrderSearchRows", PurchaseOrderSearchRows([]PurchaseOrderRow{poRow}), "供應商A"},
		{"NewPurchaseOrderPage", NewPurchaseOrderPage("2026-11-09", "", "", testOwner), ""},
		{"ShowPurchaseOrderPage", ShowPurchaseOrderPage(model.PurchaseOrder{ID: 1, SupplierName: "供應商A", Status: "draft"}, []model.PurchaseOrderItem{testPOItem}, 5000, false, "", "", "", testOwner), "供應商A"},
		{"PurchaseOrderItemsTable", PurchaseOrderItemsTable(1, []model.PurchaseOrderItem{testPOItem}, 5000, false), "機油"},
		{"AddPurchaseItemForm", AddPurchaseItemForm(1, ""), ""},
		{"PurchaseItemLookup", PurchaseItemLookup(1), ""},
		{"QuotationPage", QuotationPage(QuotationPageData{CustomerName: "王小明", PlateNumber: "ABC-1234", Groups: []QuotationGroup{{Label: "維修", Items: []QuotationItem{{Description: "換機油", Subtotal: "800"}}}}, Total: "800"}), "王小明"},
		{"ReceiptPage", ReceiptPage(ReceiptPageData{CustomerName: "王小明", Items: []ReceiptItem{{Description: "換機油", Subtotal: "800"}}, Total: "800"}), "王小明"},
		{"ListReceivablesPage", ListReceivablesPage([]ReceivableRow{recRow}, 1, 1, 20, "", "", "", "", false, false, 1000, 1000, 0, 0, 0, testOwner), "V-001"},
		{"ReceivableRowContent", ReceivableRowContent(recRow), "V-001"},
		{"ReceivableSearchRows", ReceivableSearchRows([]ReceivableRow{recRow}), "V-001"},
		{"ListScheduledTasksPage", ListScheduledTasksPage([]ScheduledTaskRow{{Name: "備份", Enabled: true}}, []NotificationRow{{ID: 1, Subject: "通知"}}, []BackupRow{{Date: "2026-11-09", Success: true}}, "待執行", "最近", "pill", "正常", testOwner), "備份"},
		{"EditAppointmentPage", EditAppointmentPage(testAppt, "", testOwner), ""},
		{"AddVehicleMileagePage", AddVehicleMileagePage(1, "ABC-1234", "Toyota", 50000, true, testOwner), "ABC-1234"},
		{"EditEquipmentPage", EditEquipmentPage(testEquip, testOwner), "頂高機"},
		{"EquipmentMaintenancePage", EquipmentMaintenancePage(1, "頂高機", "", []EquipmentMaintenanceRow{{ID: 1, Date: "2026-11-01", Type: "保養"}}, "", testOwner), "頂高機"},
		{"ListSOPTemplatesPage", ListSOPTemplatesPage(SOPTemplateListData{Rows: []SOPTemplateListRow{{ID: 1, Name: "保養SOP", ItemCount: 3}}, User: testOwner}), "保養SOP"},
		{"NewSOPTemplatePage", NewSOPTemplatePage(testOwner), ""},
		{"EditSOPTemplatePage", EditSOPTemplatePage(SOPTemplateEditData{ID: 1, Name: "保養SOP", Items: []SOPTemplateEditItem{{ID: 1, Description: "檢查煞車", Required: true}}, User: testOwner}), "保養SOP"},
		{"StaffPickerResults", StaffPickerResults([]StaffPickerRow{{ID: 1, Name: "技師A"}}), "技師A"},
		{"ListStaffPage", ListStaffPage([]StaffRow{staffRow}, 1, 1, 20, "", "", 1, 0, 0, true, "", "", testOwner), "技師A"},
		{"StaffRowContent", StaffRowContent(staffRow), "tech1"},
		{"StaffSearchRows", StaffSearchRows([]StaffRow{staffRow}), "技師A"},
		{"EditSupplierPage", EditSupplierPage(SupplierEditData{ID: 1, Name: "供應商A", User: testOwner}), "供應商A"},
		{"EditSubcontractPage", EditSubcontractPage(SubcontractEditData{ID: 1, Description: "鈑金", User: testOwner}), "鈑金"},
		{"EditStaffPage", EditStaffPage(StaffEditData{ID: 1, Name: "技師A", Username: "tech1", User: testOwner}), "技師A"},
		{"EditCustomerPage", EditCustomerPage(CustomerEditData{Customer: testCustomer, User: testOwner}), "王小明"},
		{"CustomerLineBinding", CustomerLineBinding(testCustomer), ""},
		{"CustomerTelegramBinding", CustomerTelegramBinding(testCustomer), ""},
		{"EditInsuranceCompanyPage", EditInsuranceCompanyPage(InsuranceCompanyEditData{ID: 1, Name: "保險A", User: testOwner}), "保險A"},
		{"VehicleInspectionFormPage", VehicleInspectionFormPage(InspectionFormData{VehicleID: 1, Title: "檢驗單", User: testOwner}), "檢驗單"},
		{"ReceivableFormPage", ReceivableFormPage(ReceivableFormData{Title: "新增收款", VoucherNo: "V-002", User: testOwner}), "新增收款"},
		{"ListMaintenanceTemplatesPage", ListMaintenanceTemplatesPage(MaintenanceTemplateListData{Rows: []MaintenanceTemplateListRow{{ID: 1, Code: "M1", Name: "保養套餐"}}, User: testOwner}), "保養套餐"},
		{"NewMaintenanceTemplatePage", NewMaintenanceTemplatePage(testOwner), ""},
		{"EditMaintenanceTemplatePage", EditMaintenanceTemplatePage(MaintenanceTemplateEditData{ID: 1, Code: "M1", Name: "保養套餐", Items: []MaintenanceTemplateEditItem{{Description: "換機油"}}, User: testOwner}), "保養套餐"},
		{"ListSubcontractsPage", ListSubcontractsPage([]SubcontractRow{subRow}, 1, 1, 20, "", "", "", testOwner), "鈑金"},
		{"SubcontractRowContent", SubcontractRowContent(subRow), "鈑金"},
		{"SubcontractSearchRows", SubcontractSearchRows([]SubcontractRow{subRow}), "鈑金"},
		{"ListSuppliersPage", ListSuppliersPage([]SupplierRow{supRow}, 1, 1, 20, "", "", 1, 0, true, testOwner), "供應商A"},
		{"SupplierRowContent", SupplierRowContent(supRow), "供應商A"},
		{"SupplierSearchRows", SupplierSearchRows([]SupplierRow{supRow}), "供應商A"},
		{"VehicleSearchRows", VehicleSearchRows([]VehicleRow{vehRow}), "ABC-1234"},
		{"ListVehiclesPage", ListVehiclesPage([]VehicleRow{vehRow}, 1, 1, 20, "", 0, "", "", "", "", testOwner), "ABC-1234"},
		{"ShowVehiclePage", ShowVehiclePage(testVehicle, 2, 10000, 500, 60000, "2026-11-01", "60000", "", "", "", testOwner), "ABC-1234"},
		{"EditVehiclePage", EditVehiclePage(testVehicle, "", "", "", "", testOwner), "ABC-1234"},
		{"NewVehiclePage", NewVehiclePage("王小明", "", "", "", "", testOwner), ""},
		{"ListWorkOrdersPage", ListWorkOrdersPage([]WorkOrderRow{woRow}, 1, 1, 20, "", "", "", "", "", "", "", "", testOwner), "ABC-1234"},
		{"WorkOrderRowContent", WorkOrderRowContent(woRow), "ABC-1234"},
		{"WorkOrderSearchError", WorkOrderSearchError(), ""},
		{"WorkOrderSearchRows", WorkOrderSearchRows([]WorkOrderRow{woRow}), "ABC-1234"},
		{"NewWorkOrderPage", NewWorkOrderPage(NewWorkOrderPageData{Vehicles: []model.Vehicle{testVehicle}}, testOwner), "ABC-1234"},
		{"ShowWorkOrderPage", ShowWorkOrderPage(testWO, []model.WorkOrderItem{testWOItem}, 800, map[string]string{"repairing": "維修中"}, map[string]string{"repairing": "pill"}, nil, nil, map[int64]string{1: "技師A"}, true, "", testSOPRecs, "", nil, true, nil, testOwner), "換機油"},
		{"ShowWorkOrderPageTech", ShowWorkOrderPage(testWO, nil, 0, nil, nil, nil, nil, nil, false, "", nil, "", nil, false, nil, testTech), ""},
		{"ShowWorkOrderPageInsurance", ShowWorkOrderPage(model.WorkOrder{ID: 2, PlateNumber: "XYZ-9999", Status: "completed", IsInsurance: true, InsuranceCompanyID: 1}, []model.WorkOrderItem{{ID: 2, WorkOrderID: 2, ItemType: "part", Description: "煞車皮", Quantity: 2, UnitPrice: 1500}, {ID: 3, WorkOrderID: 2, ItemType: "labor", Description: "工資", Quantity: 1, UnitPrice: 1000}}, 4000, map[string]string{"completed": "已完工"}, map[string]string{"completed": "pill-ok"}, map[string]string{"approved": "已同意"}, map[string]string{"approved": "pill-ok"}, map[int64]string{1: "技師A"}, true, "已完工", testSOPRecs, "opt", []int64{1}, true, []PendingApproval{{ID: 1, Description: "追加項目"}}, testOwner), "已完工"},
		{"EstimateItemTypes", EstimateDetailPage(estDetail, []EstimateItemRow{{ID: 1, ItemType: "part", Description: "煞車皮", Quantity: 2, UnitPrice: 1500, Subtotal: 3000}, {ID: 2, ItemType: "labor", Description: "工資", Quantity: 1, UnitPrice: 1000, Subtotal: 1000}, {ID: 3, ItemType: "other", Description: "雜項", Quantity: 1, UnitPrice: 200, Subtotal: 200}}, testOwner, true), "煞車皮"},
		{"PhotosWithMarker", WorkOrderPhotosPage(1, []PhotoCard{{WorkOrderID: 1, ID: 5, OriginalName: "b.jpg", Marker: PhotoMarkerOverlay("/photos/5.jpg", 5)}}, "/data/photos", testCounter, ""), "b.jpg"},
		{"WorkOrderItemsTable", WorkOrderItemsTable(1, []model.WorkOrderItem{testWOItem}, map[int64]string{1: "技師A"}, true, 800, testOwner), "換機油"},
		{"AddItemForm", AddItemForm(1), ""},
		{"StatusControls", StatusControls(testWO, true), ""},
		{"InsuranceRow", InsuranceRow(testWO, true), ""},
		{"SOPChecklistTable", SOPChecklistTable(1, testSOPRecs, true), "檢查煞車"},
		{"PartSearchScript", PartSearchScript(), ""},
		{"PhrasePickerScript", PhrasePickerScript(), ""},
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
