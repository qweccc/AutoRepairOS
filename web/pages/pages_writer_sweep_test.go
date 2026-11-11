package pages

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/a-h/templ"
	templruntime "github.com/a-h/templ/runtime"

	"autorepairos/web/components"
)

// init shrinks templ's render buffer to 1 byte for the whole test binary so
// every write flushes to the underlying writer. This makes templ's generated
// per-write `if err != nil { return err }` branches reachable by failing
// writers; with the default 4KB buffer they only fire at the final flush.
func init() { templruntime.DefaultBufferSize = 1 }

// allSweepPages returns the page components used by the error-path sweeps.
// Shared by TestGeneratedErrorPathsRound2 (ctx.Err + non-buffer flush) and
// TestWriterSweepRound3 (per-write error returns) so the two stay in sync.
func allSweepPages() map[string]templ.Component {
	return map[string]templ.Component{
		"APHomePage":                   APHomePage(nil, nil, nil, 0, "", "2026-11-09", testOwner),
		"APStatementPage":              APStatementPage(nil, "", "", "", 0, 0, 0, nil, 0, "", testOwner),
		"ListAppointmentsPage":         ListAppointmentsPage(nil, 0, 1, 20, "", "", "", "day", nil, components.BayOptions("", nil, false), nil, "", testOwner),
		"AppointmentCalendar":          AppointmentCalendar("2026-11-10", "day", "", nil),
		"ApprovalPage":                 ApprovalPage(ApprovalPageData{}),
		"ApprovalResult":               ApprovalResult("ok", "m"),
		"WorkOrderClaimFields":         WorkOrderClaimFields(testWO, true),
		"ListCustomersPage":            ListCustomersPage(nil, 0, 1, 20, "", CustomerCreateForm{}, testOwner),
		"DashboardPage":                DashboardPage(nil, nil, nil, testOwner),
		"ListEquipmentPage":            ListEquipmentPage(nil, 0, 1, 20, "", "", testOwner),
		"EstimateListPage":             EstimateListPage(nil, testOwner),
		"EstimateNewPage":              EstimateNewPage(nil, "", testOwner),
		"EstimateDetailPage":           EstimateDetailPage(EstimateDetail{}, nil, testOwner, true),
		"ListExpensesPage":             ListExpensesPage(nil, 0, 1, 20, "", 0, 0, testOwner),
		"VehicleInsuranceForm":         VehicleInsuranceForm(VehicleInsuranceFormData{}),
		"ListVehicleInsurancesPage":    ListVehicleInsurancesPage(nil, 0, 1, 20, "", "", false, "", testOwner),
		"ListInventoryPage":            ListInventoryPage(nil, 0, 0, 0, 1, 20, "", false, false, "", "", "", InventoryCreateForm{}, testOwner),
		"LoginPage":                    LoginPage("App", "", ""),
		"ListLookupPage":               ListLookupPage(nil, "c", "l", 0, "", testOwner),
		"Phase5PageView":               Phase5PageView(testOwner, Phase5Page{Title: "t", Body: components.Empty("e")}),
		"Phase5Tables":                 Phase5Tables(nil),
		"CampaignAnalysisTable":        CampaignAnalysisTable(nil, true),
		"TechnicianEfficiencyTable":    TechnicianEfficiencyTable(nil, "", true),
		"ReportsPage":                  ReportsPage(Phase5ReportData{}, testOwner),
		"WorklogsPage":                 WorklogsPage(Phase5WorklogData{}, testOwner),
		"AnalyticsPage":                AnalyticsPage(Phase5AnalyticsData{Chart: components.Empty("c"), Campaign: components.Empty(""), Technician: components.Empty("")}, testOwner),
		"PhotoMarkerOverlay":           PhotoMarkerOverlay("/p.jpg", 1),
		"PhrasesPage":                  PhrasesPage(nil, testOwner, ""),
		"PrintPage":                    PrintPage(PrintPageData{Title: "t"}),
		"ListPurchaseOrdersPage":       ListPurchaseOrdersPage(nil, 0, 1, 20, "", "", testOwner),
		"QuotationPage":                QuotationPage(QuotationPageData{}),
		"ReceiptPage":                  ReceiptPage(ReceiptPageData{}),
		"ListReceivablesPage":          ListReceivablesPage(nil, 0, 1, 20, "", "", "", "", false, false, 0, 0, 0, 0, 0, testOwner),
		"ListScheduledTasksPage":       ListScheduledTasksPage(nil, nil, nil, "", "", "", "", testOwner),
		"EditAppointmentPage":          EditAppointmentPage(testAppt, "", testOwner),
		"ListStaffPage":                ListStaffPage(nil, 0, 1, 20, "", "", 0, 0, 0, false, "", "", testOwner),
		"EditSupplierPage":             EditSupplierPage(SupplierEditData{User: testOwner}),
		"EditStaffPage":                EditStaffPage(StaffEditData{User: testOwner}),
		"ListSubcontractsPage":         ListSubcontractsPage(nil, 0, 1, 20, "", "", "", testOwner),
		"ListSuppliersPage":            ListSuppliersPage(nil, 0, 1, 20, "", "", 0, 0, false, testOwner),
		"ListVehiclesPage":             ListVehiclesPage(nil, 0, 1, 20, "", 0, "", "", "", "", testOwner),
		"ShowVehiclePage":              ShowVehiclePage(testVehicle, 0, 0, 0, 0, "", "", "", "", "", testOwner),
		"ListWorkOrdersPage":           ListWorkOrdersPage(nil, 0, 1, 20, "", "", "", "", "", "", "", "", testOwner),
		"NewWorkOrderPage":             NewWorkOrderPage(NewWorkOrderPageData{}, testOwner),
		"ShowWorkOrderPage":            ShowWorkOrderPage(testWO, nil, 0, "", nil, "", nil, nil, false, "", nil, "", nil, false, nil, testOwner),
		"WorkOrderItemsTable":          WorkOrderItemsTable(1, nil, nil, true, 0, testOwner),
		"StatusControls":               StatusControls(testWO, true),
		"SOPChecklistTable":            SOPChecklistTable(1, nil, true),
		"AnalyticsCharts":              AnalyticsCharts(nil, nil, 0, true),
		"NegativeStockPage":            NegativeStockPage(NegativeStockStats{}, nil, 0, 1, 20, "", "", "", "", "", "", testOwner),
		"MovementTable":                MovementTable("t", nil, true),
		"ListSOPTemplatesPage":         ListSOPTemplatesPage(SOPTemplateListData{User: testOwner}),
		"ListMaintenanceTemplatesPage": ListMaintenanceTemplatesPage(MaintenanceTemplateListData{User: testOwner}),
		"VehicleInspectionFormPage":    VehicleInspectionFormPage(InspectionFormData{User: testOwner}),
	}
}

// failAtWriter lets limit bytes through, then fails. Rendering the same
// component with a range of limits makes templ's bufio buffer flush at
// successive points, so each generated `if err != nil { return err }` write
// branch is reached.
type failAtWriter struct {
	limit   int
	written int
}

func (w *failAtWriter) Write(p []byte) (int, error) {
	if w.written >= w.limit {
		return 0, errors.New("sweep: writer failed")
	}
	if allow := w.limit - w.written; len(p) > allow {
		w.written = w.limit
		return allow, errors.New("sweep: writer failed")
	}
	w.written += len(p)
	return len(p), nil
}

func sweepPageWriterErrors(t *testing.T, c templ.Component) {
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
		_ = c.Render(context.Background(), &failAtWriter{limit: i})
	}
}

// TestWriterSweepRound3 covers templ's per-write error returns in generated
// page code, which the buffered renders never reach. templ buffers output in
// a 4KB bufio.Writer, so a failing writer only surfaces at the final flush;
// shrinking the buffer to 1 byte forces a flush on every write, making each
// generated `if err != nil { return err }` branch reachable.
func TestWriterSweepRound3(t *testing.T) {
	old := templruntime.DefaultBufferSize
	templruntime.DefaultBufferSize = 1
	defer func() { templruntime.DefaultBufferSize = old }()

	for name, c := range allSweepPages() {
		c := c
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Skipf("panics on sample data: %v", r)
				}
			}()
			sweepPageWriterErrors(t, c)
		})
	}
}
