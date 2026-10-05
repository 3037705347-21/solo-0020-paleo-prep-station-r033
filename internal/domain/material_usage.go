package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// MaterialMovementType enumerates the ledger events that change one batch.
type MaterialMovementType string

const (
	MovementIntake      MaterialMovementType = "intake"
	MovementCheckout    MaterialMovementType = "checkout"
	MovementConsumption MaterialMovementType = "consumption"
	MovementReturn      MaterialMovementType = "return"
)

// MaterialMovement is an append-only fact describing one balance change.
// Shelf balance is reserved at checkout and restored at return; consumption
// settles held stock without touching the shelf again. At every moment:
// batch.Balance == intake quantity - sum(checkout) + sum(return).
type MaterialMovement struct {
	ID           string               `json:"id"`
	BatchID      string               `json:"batch_id"`
	BatchNumber  string               `json:"batch_number"`
	PlanID       string               `json:"plan_id,omitempty"`
	Type         MaterialMovementType `json:"type"`
	Quantity     int                  `json:"quantity"`
	BalanceAfter int                  `json:"balance_after"`
	Operator     string               `json:"operator"`
	Note         string               `json:"note"`
	CreatedAt    time.Time            `json:"created_at"`
}

func NewMaterialMovement(
	id string,
	batch ConsumableBatch,
	kind MaterialMovementType,
	quantity int,
	balanceAfter int,
	planID string,
	operator string,
	note string,
	now time.Time,
) (MaterialMovement, error) {
	if id == "" {
		return MaterialMovement{}, contracts.Internal("missing_generated_id", "耗材流水标识未生成", nil)
	}
	if batch.ID == "" {
		return MaterialMovement{}, contracts.NotFound("batch_not_found", "未找到耗材批次")
	}
	switch kind {
	case MovementIntake, MovementCheckout, MovementConsumption, MovementReturn:
	default:
		return MaterialMovement{}, contracts.Internal("invalid_movement_type", "未知耗材流水类型", nil)
	}
	if err := validateMaterialQuantity(quantity); err != nil {
		return MaterialMovement{}, err
	}
	if balanceAfter < 0 || balanceAfter > batch.Quantity {
		return MaterialMovement{}, contracts.Internal("invalid_balance_after", "耗材流水余额超出批次边界", nil)
	}
	actor, err := requiredText("操作人", operator, 80)
	if err != nil {
		return MaterialMovement{}, err
	}
	noteValue, err := optionalText("操作说明", note, 500)
	if err != nil {
		return MaterialMovement{}, err
	}
	if kind != MovementIntake && planID == "" {
		return MaterialMovement{}, contracts.Internal("missing_plan_reference", "耗材领用、消耗和退料必须绑定方案", nil)
	}
	return MaterialMovement{
		ID:           id,
		BatchID:      batch.ID,
		BatchNumber:  batch.BatchNumber,
		PlanID:       planID,
		Type:         kind,
		Quantity:     quantity,
		BalanceAfter: balanceAfter,
		Operator:     actor,
		Note:         noteValue,
		CreatedAt:    now.UTC(),
	}, nil
}

// PlanMaterialUsage tracks one plan's cumulative position against one batch.
// Invariant: checked_out == consumed + returned, and held == checked_out - consumed - returned >= 0.
type PlanMaterialUsage struct {
	PlanID      string       `json:"plan_id"`
	BatchID     string       `json:"batch_id"`
	BatchNumber string       `json:"batch_number"`
	Kind        MaterialKind `json:"kind"`
	Name        string       `json:"name"`
	Unit        string       `json:"unit"`
	CheckedOut  int          `json:"checked_out"`
	Consumed    int          `json:"consumed"`
	Returned    int          `json:"returned"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// Held is the quantity currently checked out but neither consumed nor returned.
func (u PlanMaterialUsage) Held() int {
	return u.CheckedOut - u.Consumed - u.Returned
}

// IsSettled means nothing is left in the preparator's hands for this line.
func (u PlanMaterialUsage) IsSettled() bool {
	return u.Held() == 0
}

// OpenCheckout enforces one outstanding checkout per (plan, batch): a second
// checkout while stock is still held fails explicitly instead of merging.
func (u PlanMaterialUsage) OpenCheckout() bool {
	return u.Held() > 0
}

// NewPlanUsageLine opens a usage line when a plan first checks out a batch.
func NewPlanUsageLine(planID string, batch ConsumableBatch, now time.Time) PlanMaterialUsage {
	stamp := now.UTC()
	return PlanMaterialUsage{
		PlanID:      planID,
		BatchID:     batch.ID,
		BatchNumber: batch.BatchNumber,
		Kind:        batch.Kind,
		Name:        batch.Name,
		Unit:        batch.Unit,
		UpdatedAt:   stamp,
	}
}

// Checkout adds a new reservation, refusing to stack on an open one.
func (u *PlanMaterialUsage) Checkout(quantity int, now time.Time) error {
	if u.OpenCheckout() {
		return contracts.Conflict("duplicate_material_checkout", "该方案已领用此批次且尚未结清，不能重复领用")
	}
	if err := validateMaterialQuantity(quantity); err != nil {
		return err
	}
	u.CheckedOut += quantity
	u.UpdatedAt = now.UTC()
	return nil
}

// Consume records actual use against held stock only.
func (u *PlanMaterialUsage) Consume(quantity int, now time.Time) error {
	if err := validateMaterialQuantity(quantity); err != nil {
		return err
	}
	if quantity > u.Held() {
		return contracts.Conflict("material_consumption_exceeds_checkout", "消耗数量超过该方案此批次的未结余量")
	}
	u.Consumed += quantity
	u.UpdatedAt = now.UTC()
	return nil
}

// Return gives back unused held stock only.
func (u *PlanMaterialUsage) Return(quantity int, now time.Time) error {
	if err := validateMaterialQuantity(quantity); err != nil {
		return err
	}
	if quantity > u.Held() {
		return contracts.Conflict("material_return_exceeds_held", "退料数量超过该方案此批次的实际持有量")
	}
	u.Returned += quantity
	u.UpdatedAt = now.UTC()
	return nil
}
