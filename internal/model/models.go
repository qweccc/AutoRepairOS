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
	PaymentTerms  string    `json:"payment_terms,omitempty"`
	Active        int       `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
}

// VehicleInsurance 車輛投保紀錄（CAR35 Car Insurance List）。
type VehicleInsurance struct {
	ID                 int64  `json:"id"`
	VehicleID          int64  `json:"vehicle_id"`
	PlateNumber        string `json:"plate_number,omitempty"`
	InsuranceCompanyID int64  `json:"insurance_company_id,omitempty"`
	CompanyName        string `json:"company_name,omitempty"`
	CompanyNo          string `json:"company_no,omitempty"`
	InsuranceClass     string `json:"insurance_class,omitempty"`
	InsuranceClassName string `json:"insurance_class_name,omitempty"`
	LicenseDate        string `json:"license_date,omitempty"`
	CardNo             string `json:"card_no,omitempty"`
	AmountRaw          string `json:"amount_raw,omitempty"`
	Notes              string `json:"notes,omitempty"`
}

// VehicleInspection 車輛定檢排程（CAR35 Car Check List）。
type VehicleInspection struct {
	ID          int64  `json:"id"`
	VehicleID   int64  `json:"vehicle_id"`
	PlateNumber string `json:"plate_number,omitempty"`
	OrderDate   string `json:"order_date,omitempty"`
	CheckDate   string `json:"check_date,omitempty"`
	FirmNo      string `json:"firm_no,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// MaintenanceTemplate 保養套餐範本（CAR35 Maintain Record Header）。
type MaintenanceTemplate struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Notes     string `json:"notes,omitempty"`
	ItemCount int    `json:"item_count"`
}

type Customer struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Phone            string    `json:"phone"`
	LineUserID       string    `json:"line_user_id,omitempty"`
	TelegramUserID   string    `json:"telegram_user_id,omitempty"`
	Address          string    `json:"address,omitempty"`
	NotificationPref string    `json:"notification_preference"`
	Notes            string    `json:"notes,omitempty"`
	Tags             string    `json:"tags,omitempty"`
	CampaignCode     string    `json:"campaign_code,omitempty"`
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
	VehicleType           string    `json:"vehicle_type,omitempty"`
	VehicleTypeLabel      string    `json:"vehicle_type_label,omitempty"`
	DriverName            string    `json:"driver_name,omitempty"`
	DriverPhone           string    `json:"driver_phone,omitempty"`
	ContactName           string    `json:"contact_name,omitempty"`
	ContactPhone          string    `json:"contact_phone,omitempty"`
	OwnerName             string    `json:"owner_name,omitempty"`
	OwnerAddress          string    `json:"owner_address,omitempty"`
	OwnerIDNumber         string    `json:"owner_id_number,omitempty"`
	LicenseExpiryDate     string    `json:"license_expiry_date,omitempty"`
	Color                 string    `json:"color,omitempty"`
	DoorCount             int       `json:"door_count,omitempty"`
	ManufactureYear       int       `json:"manufacture_year,omitempty"`
	ManufactureDate       string    `json:"manufacture_date,omitempty"`
	LicenseIssueDate      string    `json:"license_issue_date,omitempty"`
	FuelType              string    `json:"fuel_type,omitempty"`
	FuelTypeLabel         string    `json:"fuel_type_label,omitempty"`
	Displacement          int       `json:"displacement,omitempty"`
	BodyStyle             string    `json:"body_style,omitempty"`
	BodyStyleLabel        string    `json:"body_style_label,omitempty"`
	SeatingCapacity       int       `json:"seating_capacity,omitempty"`
	CurbWeight            int       `json:"curb_weight,omitempty"`
	GrossWeight           int       `json:"gross_weight,omitempty"`
	TireSize              string    `json:"tire_size,omitempty"`
	Wheelbase             int       `json:"wheelbase,omitempty"`
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

	// 列表顯示用的零件彙總（非資料庫欄位）。
	// PartSummary 例："BE071×2、G015×1"；PartCount 是不同料號數。
	// 由 ListWorkOrders 整頁一次查回填入，見 woPartUsage 的說明。
	PartSummary string `json:"part_summary,omitempty"`
	PartCount   int    `json:"part_count,omitempty"`

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

type LookupItem struct {
	ID        int64  `json:"id"`
	Category  string `json:"category"`
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
	Active    int    `json:"active"`
}

type SimpleResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Appointment 預約排程（SRS 5.9）。
type Appointment struct {
	ID              int64     `json:"id"`
	CustomerID      int64     `json:"customer_id"`
	CustomerName    string    `json:"customer_name,omitempty"`
	VehicleID       int64     `json:"vehicle_id"`
	PlateNumber     string    `json:"plate_number,omitempty"`
	AppointmentDate string    `json:"appointment_date"`
	AppointmentTime string    `json:"appointment_time,omitempty"`
	ServiceType     string    `json:"service_type,omitempty"`
	Bay             string    `json:"bay,omitempty"` // 工位（頂高機／鈑金／烤漆…）
	Status          string    `json:"status"`
	Notes           string    `json:"notes,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Subcontract 外包加工（SRS 5.11）。
type Subcontract struct {
	ID            int64     `json:"id"`
	WorkOrderID   int64     `json:"work_order_id"`
	SupplierID    int64     `json:"supplier_id,omitempty"`
	SupplierName  string    `json:"supplier_name,omitempty"`
	Description   string    `json:"description"`
	CostAmount    float64   `json:"cost_amount"`
	ChargeAmount  float64   `json:"charge_amount"`
	Status        string    `json:"status"`
	ExpectedDate  string    `json:"expected_date,omitempty"`
	CompletedDate string    `json:"completed_date,omitempty"`
	Notes         string    `json:"notes,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Equipment 設備基本資料（SRS 5.11）。
type Equipment struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Model          string    `json:"model,omitempty"`
	SerialNumber   string    `json:"serial_number,omitempty"`
	Location       string    `json:"location,omitempty"`
	PurchaseDate   string    `json:"purchase_date,omitempty"`
	WarrantyExpiry string    `json:"warranty_expiry,omitempty"`
	Status         string    `json:"status"`
	Notes          string    `json:"notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// EquipmentMaintenance 設備保養紀錄（SRS 5.11）。
type EquipmentMaintenance struct {
	ID                 int64     `json:"id"`
	EquipmentID        int64     `json:"equipment_id"`
	EquipmentName      string    `json:"equipment_name,omitempty"`
	MaintenanceDate    string    `json:"maintenance_date"`
	MaintenanceType    string    `json:"maintenance_type,omitempty"`
	Description        string    `json:"description,omitempty"`
	Cost               float64   `json:"cost"`
	NextMaintenanceDate string   `json:"next_maintenance_date,omitempty"`
	TechnicianID       int64     `json:"technician_id,omitempty"`
	TechnicianName     string    `json:"technician_name,omitempty"`
	Notes              string    `json:"notes,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}
