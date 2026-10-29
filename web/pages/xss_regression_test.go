package pages

import (
	"bytes"
	"context"
	"html"
	"strings"
	"testing"

	"autorepairos/internal/model"
	"github.com/a-h/templ"
)

func renderForXSS(t *testing.T, component templ.Component) string {
	t.Helper()
	var out bytes.Buffer
	if err := component.Render(context.Background(), &out); err != nil {
		t.Fatalf("render: %v", err)
	}
	return out.String()
}

func TestTemplDynamicValuesEscapeXSS(t *testing.T) {
	injected := `<script>alert(1)</script>`
	escaped := html.EscapeString(injected)
	user := model.User{Username: injected, Role: "owner"}

	tests := []struct {
		name string
		out  string
	}{
		{
			name: "vehicle search",
			out:  renderForXSS(t, VehicleSearchRows([]VehicleRow{{PlateNumber: injected, CustomerName: injected}})),
		},
		{
			name: "customer tags",
			out:  renderForXSS(t, CustomerSearchRows([]CustomerRow{{Name: injected, Tags: injected}}, user)),
		},
		{
			name: "receipt",
			out: renderForXSS(t, ReceiptPage(ReceiptPageData{
				AppName: injected, CustomerName: injected, Complaint: injected,
				Items: []ReceiptItem{{Description: injected}},
			})),
		},
		{
			name: "converted edit form",
			out:  renderForXSS(t, EditEquipmentPage(model.Equipment{Name: injected, Notes: injected}, user)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.out, injected) {
				t.Fatalf("raw script payload remained in output: %q", tt.out)
			}
			if !strings.Contains(tt.out, escaped) {
				t.Fatalf("escaped payload missing from output: %q", tt.out)
			}
		})
	}
}
