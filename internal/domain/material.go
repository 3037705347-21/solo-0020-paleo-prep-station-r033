package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type MaterialBatch struct {
	ID           string    `json:"id"`
	MaterialKind string    `json:"material_kind"`
	BatchNumber  string    `json:"batch_number"`
	Unit         string    `json:"unit"`
	Received     int       `json:"received"`
	Remaining    int       `json:"remaining"`
	ExpiresAt    time.Time `json:"expires_at"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func NewMaterialBatch(
	id string,
	materialKind string,
	batchNumber string,
	unit string,
	quantity int,
	expiresAt time.Time,
	now time.Time,
) (MaterialBatch, error) {
	if id == "" {
		return MaterialBatch{}, contracts.Internal("missing_generated_id", "批次标识未生成", nil)
	}
	kind, err := requiredText("耗材类别", materialKind, 60)
	if err != nil {
		return MaterialBatch{}, err
	}
	number, err := validateBatchNumber(batchNumber)
	if err != nil {
		return MaterialBatch{}, err
	}
	unitValue, err := requiredText("计量单位", unit, 20)
	if err != nil {
		return MaterialBatch{}, err
	}
	if quantity <= 0 {
		return MaterialBatch{}, contracts.Invalid("invalid_quantity", "入库数量必须大于零")
	}
	if expiresAt.IsZero() {
		return MaterialBatch{}, contracts.Invalid("required_field", "有效期不能为空")
	}
	stamp := now.UTC()
	return MaterialBatch{
		ID:           id,
		MaterialKind: kind,
		BatchNumber:  number,
		Unit:         unitValue,
		Received:     quantity,
		Remaining:    quantity,
		ExpiresAt:    expiresAt.UTC(),
		Version:      1,
		CreatedAt:    stamp,
		UpdatedAt:    stamp,
	}, nil
}

func (b MaterialBatch) IsExpired(now time.Time) bool {
	return !b.ExpiresAt.After(now.UTC())
}

func (b MaterialBatch) IsDepleted() bool {
	return b.Remaining <= 0
}

func (b *MaterialBatch) Issue(quantity int, now time.Time) error {
	if quantity <= 0 {
		return contracts.Invalid("invalid_quantity", "领用数量必须大于零")
	}
	if b.IsExpired(now) {
		return contracts.Conflict("batch_expired", "批次已过有效期，不能被方案引用")
	}
	if b.IsDepleted() {
		return contracts.Conflict("batch_depleted", "批次已耗尽，不能继续领用")
	}
	if b.Remaining < quantity {
		return contracts.Conflict("insufficient_batch_balance", "批次剩余数量不足，不能满足领用数量")
	}
	b.Remaining -= quantity
	b.Version++
	b.UpdatedAt = now.UTC()
	return nil
}

func (b *MaterialBatch) Restock(quantity int, now time.Time) error {
	if quantity <= 0 {
		return contracts.Invalid("invalid_quantity", "退料数量必须大于零")
	}
	if b.Remaining+quantity > b.Received {
		return contracts.Internal("batch_balance_overflow", "退料后批次余额超过入库数量", nil)
	}
	b.Remaining += quantity
	b.Version++
	b.UpdatedAt = now.UTC()
	return nil
}

type MaterialUsage struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"plan_id"`
	BatchID   string    `json:"batch_id"`
	Issued    int       `json:"issued"`
	Consumed  int       `json:"consumed"`
	Returned  int       `json:"returned"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewMaterialUsage(
	id string,
	plan PrepPlan,
	batch MaterialBatch,
	quantity int,
	now time.Time,
) (MaterialUsage, error) {
	if id == "" {
		return MaterialUsage{}, contracts.Internal("missing_generated_id", "用量标识未生成", nil)
	}
	if plan.Status != PlanApproved && plan.Status != PlanActive {
		return MaterialUsage{}, contracts.Conflict("plan_not_issuable", "只有已审批或进行中的方案可以领用耗材")
	}
	if quantity <= 0 {
		return MaterialUsage{}, contracts.Invalid("invalid_quantity", "领用数量必须大于零")
	}
	stamp := now.UTC()
	return MaterialUsage{
		ID:        id,
		PlanID:    plan.ID,
		BatchID:   batch.ID,
		Issued:    quantity,
		CreatedAt: stamp,
		UpdatedAt: stamp,
	}, nil
}

func (u MaterialUsage) Held() int {
	return u.Issued - u.Consumed - u.Returned
}

func (u MaterialUsage) IsSettled() bool {
	return u.Held() == 0
}

func (u *MaterialUsage) Consume(quantity int, now time.Time) error {
	if quantity <= 0 {
		return contracts.Invalid("invalid_quantity", "消耗数量必须大于零")
	}
	if u.Held() < quantity {
		return contracts.Conflict("usage_overdraw", "消耗数量超过方案当前持有的耗材数量")
	}
	u.Consumed += quantity
	u.UpdatedAt = now.UTC()
	return nil
}

func (u *MaterialUsage) Return(quantity int, now time.Time) error {
	if quantity <= 0 {
		return contracts.Invalid("invalid_quantity", "退料数量必须大于零")
	}
	if u.Held() < quantity {
		return contracts.Conflict("return_exceeds_held", "退料数量超过方案当前持有的耗材数量")
	}
	u.Returned += quantity
	u.UpdatedAt = now.UTC()
	return nil
}
