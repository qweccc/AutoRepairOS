package model

import "time"

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Role      string    `json:"role"`
	Name      string    `json:"name"`
	Active    int       `json:"active"`
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
	Birthday         string    `json:"birthday,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	Tags             string    `json:"tags,omitempty"`
	CampaignCode     string    `json:"campaign_code,omitempty"`
	TotalSpent       float64   `json:"total_spent"`
	UnpaidAmount     float64   `json:"unpaid_amount"`
	CreatedAt        time.Time `json:"created_at"`
}

type Vehicle struct {
	ID                     int64     `json:"id"`
	CustomerID             int64     `json:"customer_id"`
	CustomerName           string    `json:"customer_name,omitempty"`
	PlateNumber            string    `json:"plate_number"`
	BrandModel             string    `json:"brand_model"`
	VehicleType            string    `json:"vehicle_type,omitempty"`
	VehicleTypeLabel       string    `json:"vehicle_type_label,omitempty"`
	DriverName             string    `json:"driver_name,omitempty"`
	DriverPhone            string    `json:"driver_phone,omitempty"`
	ContactName            string    `json:"contact_name,omitempty"`
	ContactPhone           string    `json:"contact_phone,omitempty"`
	OwnerName              string    `json:"owner_name,omitempty"`
	OwnerAddress           string    `json:"owner_address,omitempty"`
	OwnerIDNumber          string    `json:"owner_id_number,omitempty"`
	LicenseExpiryDate      string    `json:"license_expiry_date,omitempty"`
	Color                  string    `json:"color,omitempty"`
	DoorCount              int       `json:"door_count,omitempty"`
	ManufactureYear        int       `json:"manufacture_year,omitempty"`
	ManufactureDate        string    `json:"manufacture_date,omitempty"`
	LicenseIssueDate       string    `json:"license_issue_date,omitempty"`
	FuelType               string    `json:"fuel_type,omitempty"`
	FuelTypeLabel          string    `json:"fuel_type_label,omitempty"`
	Displacement           int       `json:"displacement,omitempty"`
	BodyStyle              string    `json:"body_style,omitempty"`
	BodyStyleLabel         string    `json:"body_style_label,omitempty"`
	SeatingCapacity        int       `json:"seating_capacity,omitempty"`
	CurbWeight             int       `json:"curb_weight,omitempty"`
	GrossWeight            int       `json:"gross_weight,omitempty"`
	TireSize               string    `json:"tire_size,omitempty"`
	Wheelbase              int       `json:"wheelbase,omitempty"`
	VIN                    string    `json:"vin,omitempty"`
	EngineNumber           string    `json:"engine_number,omitempty"`
	InspectionDueDate      string    `json:"inspection_due_date,omitempty"`
	NextMaintenanceDate    string    `json:"next_maintenance_date,omitempty"`
	NextMaintenanceMileage int       `json:"next_maintenance_mileage,omitempty"`
	LastMaintenanceDate    string    `json:"last_maintenance_date,omitempty"`
	LastMaintenanceMileage int       `json:"last_maintenance_mileage,omitempty"`
	MaintIntervalMonths    int       `json:"next_maintenance_interval_months"`
	MaintIntervalKm        int       `json:"next_maintenance_interval_km"`
	CreatedAt              time.Time `json:"created_at"`
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
	Category       string  `json:"category,omitempty"` // 零件類別（引擎/電機音響/…）
	ItemKind       string  `json:"item_kind"`          // part 零件 / labor 工資服務
}

type WorkOrder struct {
	ID                int64   `json:"id"`
	VehicleID         int64   `json:"vehicle_id"`
	CustomerID        int64   `json:"customer_id"`
	TechnicianID      int64   `json:"technician_id,omitempty"`
	PlateNumber       string  `json:"plate_number,omitempty"`
	BrandModel        string  `json:"brand_model,omitempty"`
	CustomerName      string  `json:"customer_name,omitempty"`
	CustomerPhone     string  `json:"customer_phone,omitempty"`
	TechnicianName    string  `json:"technician_name,omitempty"`
	Status            string  `json:"status"`
	CurrentMileage    int     `json:"current_mileage"`
	CustomerComplaint string  `json:"customer_complaint,omitempty"`
	ApprovalStatus    string  `json:"approval_status"`
	TotalAmount       float64 `json:"total_amount"`
	SOPCompleted      bool    `json:"sop_completed"`
	Notes             string  `json:"notes,omitempty"`

	// 保險理賠（CAR35 P0-1，對應 Header.Insurance）
	// InsuranceCompanyID 與 IsInsurance 分開：車主有保險但本次自費維修很常見，
	// 且報表要能只篩 is_insurance 而不必 JOIN insurance_companies。
	InsuranceCompanyID int64   `json:"insurance_company_id,omitempty"`
	InsuranceName      string  `json:"insurance_name,omitempty"`
	ClaimNo            string  `json:"claim_no,omitempty"`
	IsInsurance        bool    `json:"is_insurance"`
	AccidentDate       string  `json:"accident_date,omitempty"`
	DeductibleAmount   float64 `json:"deductible_amount"`
	ClaimAmount        float64 `json:"claim_amount"`
	ClaimDate          string  `json:"claim_date,omitempty"`
	ClaimNote          string  `json:"claim_note,omitempty"`

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

// Passbook 車輛維修履歷與保養收據（SRS FR-VH-05：LINE 數位維修履歷卡）。
//
// 綁定 LINE 的客戶可透過公開端點查詢愛車歷次維修/保養紀錄與收據明細，
// 無需登入。身份驗證靠 customers.line_user_id 綁定，不是靠登入 session，
// 因此本 handler 不需要 RequireLogin / AllStaff 等 middleware。
type Passbook struct {
	ID              int64     `json:"id"`
	CustomerID      int64     `json:"customer_id"`
	CustomerName    string    `json:"customer_name"`
	VehicleID       int64     `json:"vehicle_id"`
	VehiclePlate    string    `json:"vehicle_plate"`
	WorkOrderID     int64     `json:"work_order_id"`
	WorkOrderNo     string    `json:"work_order_no,omitempty"`
	ServiceType     string    `json:"service_type"` // inspection 預檢 / maintenance 保養 / repair 維修 / parts 零件
	Description     string    `json:"description"`
	PartsCost       float64   `json:"parts_cost"`
	LaborCost       float64   `json:"labor_cost"`
	SubcontractCost float64   `json:"subcontract_cost"` // 外包成本（表有此欄，Scan 需對應）
	Subtotal        float64   `json:"subtotal"`
	Tax             float64   `json:"tax"`
	DiscountAmount  float64   `json:"discount_amount"`
	TotalAmount     float64   `json:"total_amount"`
	Status          string    `json:"status"` // pending 待付款 / approved 已核准 / paid 已付款 / rejected 已拒絕
	Notes           string    `json:"notes"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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

// PurchaseOrder 進貨單主檔：記錄向供應商進貨，核心是「總共要給廠商多少錢」。
type PurchaseOrder struct {
	ID           int64   `json:"id"`
	SupplierID   int64   `json:"supplier_id"`
	SupplierName string  `json:"supplier_name,omitempty"`
	WorkOrderID  int64   `json:"work_order_id,omitempty"` // 0 = 一般進貨；>0 = 為此工單叫的料
	OrderDate    string  `json:"order_date,omitempty"`
	Status       string  `json:"status"` // draft / received
	TotalAmount  float64 `json:"total_amount"`
	Notes        string  `json:"notes,omitempty"`
	ItemCount    int     `json:"item_count,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
	ReceivedAt   string  `json:"received_at,omitempty"`
}

// PurchaseOrderItem 進貨單品項：數量 × 進貨單價 = 小計。
// Category 是拍照辨識時猜到的零件類別（lookup code）；確認進貨自動建檔時帶到新零件。
type PurchaseOrderItem struct {
	ID              int64   `json:"id"`
	OrderID         int64   `json:"order_id"`
	InventoryItemID int64   `json:"inventory_item_id,omitempty"`
	Description     string  `json:"description"`
	PartNumber      string  `json:"part_number,omitempty"`
	Category        string  `json:"category,omitempty"`
	Quantity        float64 `json:"quantity"`
	UnitCost        float64 `json:"unit_cost"`
	Subtotal        float64 `json:"subtotal"`
}

// APPayable 供應商應付帳款。金額在來源成立時快照，不隨來源單據後續編輯變動。
type APPayable struct {
	ID         int64   `json:"id"`
	SourceType string  `json:"source_type"`
	SourceID   int64   `json:"source_id"`
	SupplierID int64   `json:"supplier_id"`
	Amount     float64 `json:"amount"`
	PaidAmount float64 `json:"paid_amount"`
	Balance    float64 `json:"balance"`
	DueDate    string  `json:"due_date"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	PaidAt     string  `json:"paid_at,omitempty"`
}

// APPayment 應付帳款的單次付款流水。
type APPayment struct {
	ID        int64   `json:"id"`
	PayableID int64   `json:"payable_id"`
	Amount    float64 `json:"amount"`
	PaidAt    string  `json:"paid_at"`
}

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

// SOPTemplate SOP 檢查表模板（FR-WO-08）。
type SOPTemplate struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	RepairType string `json:"repair_type,omitempty"` // maintenance/overhaul/bodywork/…
	Active     int    `json:"active"`
	SortOrder  int    `json:"sort_order"`
	ItemCount  int    `json:"item_count,omitempty"` // 非 DB 欄位：查詢時計算
}

// SOPItem SOP 模板內的檢查項目。
type SOPItem struct {
	ID          int64  `json:"id"`
	TemplateID  int64  `json:"template_id"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	SortOrder   int    `json:"sort_order"`
}

// SOPRecord 工單逐項 SOP 勾選紀錄（含完成者與完成時間追溯）。
type SOPRecord struct {
	ID              int64  `json:"id"`
	WorkOrderID     int64  `json:"work_order_id"`
	SOPItemID       int64  `json:"sop_item_id"`
	TemplateID      int64  `json:"template_id"`
	Description     string `json:"description"`
	Required        bool   `json:"required"`
	Completed       bool   `json:"completed"`
	CompletedBy     int64  `json:"completed_by,omitempty"`
	CompletedByName string `json:"completed_by_name,omitempty"` // 非 DB 欄位：JOIN 取得
	CompletedAtText string `json:"completed_at_text,omitempty"` // 非 DB 欄位：格式化後的的操作時間
	SortOrder       int    `json:"sort_order"`
}

// PhotoAnnotation 照片標記（Canvas 劃線/圈選/箭頭/文字標註）。
type PhotoAnnotation struct {
	ID             int64   `json:"id"`
	PhotoID        int64   `json:"photo_id"`
	WorkOrderID    int64   `json:"work_order_id"`
	AnnotationType string  `json:"annotation_type"`
	X1             float64 `json:"x1"`
	Y1             float64 `json:"y1"`
	X2             float64 `json:"x2"`
	Y2             float64 `json:"y2"`
	Color          string  `json:"color"`
	Note           string  `json:"note"`
	CreatedBy      int64   `json:"created_by,omitempty"`
	CreatedAt      string  `json:"created_at,omitempty"`
}

// WalkaroundMark 車身舊傷預檢標記（FR-VH-06）。
type WalkaroundMark struct {
	ID          int64   `json:"id"`
	WorkOrderID int64   `json:"work_order_id"`
	VehicleID   int64   `json:"vehicle_id"`
	MarkType    string  `json:"mark_type"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Note        string  `json:"note,omitempty"`
	PhotoID     int64   `json:"photo_id,omitempty"`
	CreatedBy   int64   `json:"created_by,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
}

// EquipmentMaintenance 設備保養紀錄（SRS 5.11）。
type EquipmentMaintenance struct {
	ID                  int64     `json:"id"`
	EquipmentID         int64     `json:"equipment_id"`
	EquipmentName       string    `json:"equipment_name,omitempty"`
	MaintenanceDate     string    `json:"maintenance_date"`
	MaintenanceType     string    `json:"maintenance_type,omitempty"`
	Description         string    `json:"description,omitempty"`
	Cost                float64   `json:"cost"`
	NextMaintenanceDate string    `json:"next_maintenance_date,omitempty"`
	TechnicianID        int64     `json:"technician_id,omitempty"`
	TechnicianName      string    `json:"technician_name,omitempty"`
	Notes               string    `json:"notes,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// TechnicianPhrase 技師常用片語（FR-WO-07）。
type TechnicianPhrase struct {
	ID        int64  `json:"id"`
	Phrase    string `json:"phrase"`
	Category  string `json:"category,omitempty"`
	SortOrder int    `json:"sort_order"`
	Active    int    `json:"active"`
	CreatedAt string `json:"created_at,omitempty"`
}

// ApprovalRequest 追加維修授權紀錄（FR-WO-06、FR-NOTI-05）。
type ApprovalRequest struct {
	ID             int64  `json:"id"`
	WorkOrderID    int64  `json:"work_order_id"`
	CustomerID     int64  `json:"customer_id"`
	Token          string `json:"token"`
	ItemSummary    string `json:"item_summary,omitempty"`
	Status         string `json:"status"` // pending/approved/rejected/expired
	ExpiresAt      string `json:"expires_at"`
	CreatedAt      string `json:"created_at,omitempty"`
	DecidedAt      string `json:"decided_at,omitempty"`
	DecisionSource string `json:"decision_source,omitempty"`
	Notes          string `json:"notes,omitempty"`
}
