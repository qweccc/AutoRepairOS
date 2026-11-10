package handler

// WorkOrderStatus represents the lifecycle status of a work order.
type WorkOrderStatus string

const (
	WorkOrderStatusPending      WorkOrderStatus = "pending"
	WorkOrderStatusInProgress   WorkOrderStatus = "in_progress"
	WorkOrderStatusWaitingParts WorkOrderStatus = "waiting_parts"
	WorkOrderStatusCompleted    WorkOrderStatus = "completed"
	WorkOrderStatusPaid         WorkOrderStatus = "paid"
)

// IsMutable reports whether the work order can still be edited.
func (s WorkOrderStatus) IsMutable() bool {
	return s != WorkOrderStatusPaid
}
