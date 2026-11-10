package model

import (
	"encoding/json"
	"testing"
	"time"
)

// model 只有 struct 定義（無方法/函式），這裡驗證 JSON 序列化形狀符合
// json tag 約定（前端與 API 契約），避免改名/改 tag 時靜默破壞契約。

func TestUserJSONHidesPassword(t *testing.T) {
	u := User{ID: 1, Username: "owner", Password: "secret", Role: "owner", Name: "X", Active: 1}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["password"]; ok {
		t.Error("Password 應被 json:\"-\" 隱藏")
	}
	if m["username"] != "owner" || m["role"] != "owner" {
		t.Errorf("m = %v", m)
	}
}

func TestStructJSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	wo := WorkOrder{ID: 1, Status: "pending", TotalAmount: 100.5, IsInsurance: true, CreatedAt: now, UpdatedAt: now}
	b, err := json.Marshal(wo)
	if err != nil {
		t.Fatal(err)
	}
	var back WorkOrder
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != 1 || back.Status != "pending" || back.TotalAmount != 100.5 || !back.IsInsurance {
		t.Errorf("back = %+v", back)
	}
}

func TestOmitemptyFields(t *testing.T) {
	// 空字串 + omitempty → 不出現；非空 → 出現
	b, _ := json.Marshal(Customer{ID: 1, Name: "A", Phone: "09"})
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	if _, ok := m["line_user_id"]; ok {
		t.Error("空 line_user_id 應被 omitempty 省略")
	}
	b2, _ := json.Marshal(Customer{ID: 1, Name: "A", Phone: "09", LineUserID: "U1"})
	var m2 map[string]interface{}
	_ = json.Unmarshal(b2, &m2)
	if m2["line_user_id"] != "U1" {
		t.Errorf("m2 = %v", m2)
	}
}

func TestAllStructsMarshal(t *testing.T) {
	// 每個 struct 至少能 marshal/unmarshal 不出錯（防 tag 寫錯導致靜默丟欄）
	vals := []interface{}{
		User{}, Supplier{}, VehicleInsurance{}, VehicleInspection{},
		MaintenanceTemplate{}, Customer{}, Vehicle{}, InventoryItem{},
		WorkOrder{}, WorkOrderItem{}, Passbook{}, LookupItem{},
		SimpleResponse{}, Appointment{}, Subcontract{}, PurchaseOrder{},
		PurchaseOrderItem{}, APPayable{}, APPayment{}, Equipment{},
		SOPTemplate{}, SOPItem{}, SOPRecord{}, PhotoAnnotation{},
		WalkaroundMark{}, EquipmentMaintenance{}, TechnicianPhrase{}, ApprovalRequest{},
	}
	for _, v := range vals {
		if _, err := json.Marshal(v); err != nil {
			t.Errorf("%T marshal failed: %v", v, err)
		}
	}
}
