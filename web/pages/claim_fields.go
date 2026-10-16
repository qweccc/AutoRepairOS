package pages

import (
	"context"
	"fmt"
	"html"
	"io"
	"strconv"

	"autorepairos/internal/model"
	"autorepairos/web/components"
	"github.com/a-h/templ"
)

type workOrderClaimFields struct {
	workOrder model.WorkOrder
	canEdit   bool
}

func (c workOrderClaimFields) Render(ctx context.Context, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `<tr><td colspan="4">`); err != nil {
		return err
	}
	if c.canEdit {
		if _, err := fmt.Fprintf(w, `<form class="frm" method="POST" action="/work-orders/%d/claim">`, c.workOrder.ID); err != nil {
			return err
		}
		if err := components.CSRFInput().Render(ctx, w); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, `<label>理賠日期<input type="date" name="claim_date" value="%s"></label>
			<label>理賠金額<input type="number" name="claim_amount" min="0" step="0.01" value="%s"></label>
			<label class="full">理賠備註<textarea name="claim_note">%s</textarea></label>
			<div class="full"><button class="btn btn-s" type="submit">儲存理賠資料</button></div>
			</form>`,
			html.EscapeString(c.workOrder.ClaimDate),
			html.EscapeString(strconv.FormatFloat(c.workOrder.ClaimAmount, 'f', 2, 64)),
			html.EscapeString(c.workOrder.ClaimNote)); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, `<b>理賠日期</b> %s　<b>理賠金額</b> %s`,
			html.EscapeString(c.workOrder.ClaimDate), html.EscapeString(money(c.workOrder.ClaimAmount))); err != nil {
			return err
		}
		if c.workOrder.ClaimNote != "" {
			if _, err := fmt.Fprintf(w, `<p>%s</p>`, html.EscapeString(c.workOrder.ClaimNote)); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, `</td></tr>`)
	return err
}

func WorkOrderClaimFields(wo model.WorkOrder, canEdit bool) templ.Component {
	return workOrderClaimFields{workOrder: wo, canEdit: canEdit}
}
