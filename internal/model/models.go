package model

import "time"

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Role      string    `json:"role"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Supplier struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Phone         string    `json:"phone,omitempty"`
	ContactPerson string    `json:"contact_person,omitempty"`
	Address       string    `json:"address,omitempty"`
	Notes         string    `json:"notes,omitempty"`
	VendorType    string    `json:"vendor_type"` // material 材料商 / vendor 協力廠商
	CreatedAt     time.Time `json:"created_at"`
}

type Customer struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Phone            string    `json:"phone"`
	LineUserID       string    `json:"line_user_id,omitempty"`
	Address          string    `json:"address,omitempty"`
	NotificationPref string    `json:"notification_preference"`
	Notes            string    `json:"notes,omitempty"`
	TotalSpent       float64   `json:"total_spent"`
	UnpaidAmount     float64   `json:"unpaid_amount"`
	CreatedAt        time.Time `json:"created_at"`
}

type Vehicle struct {
	ID                    int64     `json:"id"`
	CustomerID            int64     `json:"customer_id"`
	CustomerName          string    `json:"customer_name,omitempty"`
	PlateNumber           string    `json:"plate_number"`
	BrandModel            string    `json:"brand_model"`
	ManufactureYear       int       `json:"manufacture_year,omitempty"`
	VIN                   string    `json:"vin,omitempty"`
	EngineNumber          string    `json:"engine_number,omitempty"`
	InspectionDueDate     string    `json:"inspection_due_date,omitempty"`
	NextMaintenanceDate   string    `json:"next_maintenance_date,omitempty"`
	NextMaintenanceMileage int      `json:"next_maintenance_mileage,omitempty"`
	LastMaintenanceDate   string    `json:"last_maintenance_date,omitempty"`
	LastMaintenanceMileage int      `json:"last_maintenance_mileage,omitempty"`
	MaintIntervalMonths   int       `json:"next_maintenance_interval_months"`
	MaintIntervalKm       int       `json:"next_maintenance_interval_km"`
	CreatedAt             time.Time `json:"created_at"`
}

type InventoryItem struct {
	ID             int64   `json:"id"`
	SupplierID     int64   `json:"supplier_id,omitempty"`
	SupplierName   string  `json:"supplier_name,omitempty"`
	PartNumber     string  `json:"part_number"`
	Barcode        string  `json:"barcode,omitempty"`
	Name           string  `json:"name"`
	BinLocation    string  `json:"bin_location"`
	CostPrice      float64 `json:"cost_price"`
	SellingPrice   float64 `json:"selling_price"`
	StockQuantity  int     `json:"stock_quantity"`
	MinSafetyStock int     `json:"min_safety_stock"`
	LowStock       bool    `json:"low_stock"`
	Category       string  `json:"category,omitempty"`  // 零件類別（引擎/電機音響/…）
	ItemKind       string  `json:"item_kind"`           // part 零件 / labor 工資服務
}

type WorkOrder struct {
	ID                int64     `json:"id"`
	VehicleID         int64     `json:"vehicle_id"`
	CustomerID        int64     `json:"customer_id"`
	TechnicianID      int64     `json:"technician_id,omitempty"`
	PlateNumber       string    `json:"plate_number,omitempty"`
	BrandModel        string    `json:"brand_model,omitempty"`
	CustomerName      string    `json:"customer_name,omitempty"`
	CustomerPhone     string    `json:"customer_phone,omitempty"`
	TechnicianName    string    `json:"technician_name,omitempty"`
	Status            string    `json:"status"`
	CurrentMileage    int       `json:"current_mileage"`
	CustomerComplaint string    `json:"customer_complaint,omitempty"`
	ApprovalStatus    string    `json:"approval_status"`
	TotalAmount       float64   `json:"total_amount"`
	SOPCompleted      bool      `json:"sop_completed"`
	Notes             string    `json:"notes,omitempty"`

	// 保險理賠（CAR35 P0-1，對應 Header.Insurance）
	// InsuranceCompanyID 與 IsInsurance 分開：車主有保險但本次自費維修很常見，
	// 且報表要能只篩 is_insurance 而不必 JOIN insurance_companies。
	InsuranceCompanyID int64   `json:"insurance_company_id,omitempty"`
	InsuranceName      string  `json:"insurance_name,omitempty"`
	ClaimNo            string  `json:"claim_no,omitempty"`
	IsInsurance       bool    `json:"is_insurance"`
	AccidentDate       string  `json:"accident_date,omitempty"`
	DeductibleAmount   float64 `json:"deductible_amount"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type WorkOrderItem struct {
	ID                  int64   `json:"id"`
	WorkOrderID         int64   `json:"work_order_id"`
	ItemType            string  `json:"item_type"`
	InventoryItemID     int64   `json:"inventory_item_id,omitempty"`
	Description         string  `json:"description"`
	Quantity            int     `json:"quantity"`
	UnitPrice           float64 `json:"unit_price"`
	Subtotal            float64 `json:"subtotal"`
	PrimaryTechnicianID int64   `json:"primary_technician_id,omitempty"`
	AssistantTechID     int64   `json:"assistant_tech_id,omitempty"`
	PhotoURL            string  `json:"photo_url,omitempty"`
}

type SimpleResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}
