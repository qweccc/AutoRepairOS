package model

import (
	"time"
)

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Role      string    `json:"role"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Customer struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Phone             string    `json:"phone"`
	LineUserID        string    `json:"line_user_id,omitempty"`
	Address           string    `json:"address,omitempty"`
	NotificationPref  string    `json:"notification_preference"`
	Notes             string    `json:"notes,omitempty"`
	TotalSpent        float64   `json:"total_spent"`
	UnpaidAmount      float64   `json:"unpaid_amount"`
	CreatedAt         time.Time `json:"created_at"`
}

type WorkOrder struct {
	ID              int64     `json:"id"`
	VehicleID       int64     `json:"vehicle_id"`
	Status          string    `json:"status"`
	CurrentMileage  int       `json:"current_mileage"`
	CustomerComplaint string  `json:"customer_complaint"`
	ApprovalStatus  string    `json:"approval_status"`
	TotalAmount     float64   `json:"total_amount"`
	SOPCompleted    bool      `json:"sop_completed"`
	CustomerID      int64     `json:"customer_id"`
	TechnicianID    int64     `json:"technician_id,omitempty"`
	Notes           string    `json:"notes,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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
