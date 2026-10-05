package domain

import (
	"strings"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// MaterialKind classifies the consumables used on the preparation bench.
type MaterialKind string

const (
	MaterialAdhesive    MaterialKind = "adhesive"    // 胶结剂
	MaterialConsolidant MaterialKind = "consolidant" // 加固液
	MaterialEmbedding   MaterialKind = "embedding"   // 包埋材料
)

// BatchStatus is derived from shelf balance and expiry; it is never stored.
type BatchStatus string

const (
	BatchAvailable BatchStatus = "available"
	BatchExpired   BatchStatus = "expired"
	BatchExhausted BatchStatus = "exhausted"
)

// ConsumableBatch is the single balance authority for one intake lot.
// Quantity is fixed at intake; Balance only moves through checkout/return.
type ConsumableBatch struct {
	ID          string       `json:"id"`
	Kind        MaterialKind `json:"kind"`
	Name        string       `json:"name"`
	BatchNumber string       `json:"batch_number"`
	Quantity    int          `json:"quantity"`
	Balance     int          `json:"balance"`
	Unit        string       `json:"unit"`
	ReceivedBy  string       `json:"received_by"`
	ExpiresAt   time.Time    `json:"expires_at"`
	ReceivedAt  time.Time    `json:"received_at"`
	Version     int          `json:"version"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func ParseMaterialKind(value string) (MaterialKind, error) {
	switch MaterialKind(strings.TrimSpace(value)) {
	case MaterialAdhesive:
		return MaterialAdhesive, nil
	case MaterialConsolidant:
		return MaterialConsolidant, nil
	case MaterialEmbedding:
		return MaterialEmbedding, nil
	default:
		return "", contracts.Invalid(
			"invalid_material_kind",
			"耗材类型必须是 adhesive（胶结剂）、consolidant（加固液）或 embedding（包埋材料）",
		)
	}
}

func NewConsumableBatch(
	id string,
	kind MaterialKind,
	name string,
	batchNumber string,
	unit string,
	quantity int,
	receivedBy string,
	expiresAt time.Time,
	now time.Time,
) (ConsumableBatch, error) {
	if id == "" {
		return ConsumableBatch{}, contracts.Internal("missing_generated_id", "耗材批次标识未生成", nil)
	}
	if _, err := ParseMaterialKind(string(kind)); err != nil {
		return ConsumableBatch{}, err
	}
	materialName, err := requiredText("耗材名称", name, 120)
	if err != nil {
		return ConsumableBatch{}, err
	}
	number, err := validateBatchNumber(batchNumber)
	if err != nil {
		return ConsumableBatch{}, err
	}
	materialUnit, err := requiredText("计量单位", unit, 8)
	if err != nil {
		return ConsumableBatch{}, err
	}
	if err := validateMaterialQuantity(quantity); err != nil {
		return ConsumableBatch{}, err
	}
	receiver, err := requiredText("入库人", receivedBy, 80)
	if err != nil {
		return ConsumableBatch{}, err
	}
	if expiresAt.IsZero() {
		return ConsumableBatch{}, contracts.Invalid("invalid_expires_at", "耗材批次必须提供过期时间")
	}
	stamp := now.UTC()
	if !expiresAt.UTC().After(stamp) {
		return ConsumableBatch{}, contracts.Invalid("batch_already_expired", "入库批次不能已经过期")
	}
	return ConsumableBatch{
		ID:          id,
		Kind:        kind,
		Name:        materialName,
		BatchNumber: number,
		Quantity:    quantity,
		Balance:     quantity,
		Unit:        materialUnit,
		ReceivedBy:  receiver,
		ExpiresAt:   expiresAt.UTC(),
		ReceivedAt:  stamp,
		Version:     1,
		CreatedAt:   stamp,
		UpdatedAt:   stamp,
	}, nil
}

// Status derives whether the lot may currently be referenced by a plan.
func (b ConsumableBatch) Status(now time.Time) BatchStatus {
	if !now.UTC().Before(b.ExpiresAt) {
		return BatchExpired
	}
	if b.Balance <= 0 {
		return BatchExhausted
	}
	return BatchAvailable
}

// EnsureNotExpired blocks any continued use of an expired lot, including
// consumption of stock that was checked out before expiry.
func (b ConsumableBatch) EnsureNotExpired(now time.Time) error {
	if !now.UTC().Before(b.ExpiresAt) {
		return contracts.Conflict("batch_expired", "耗材批次 "+b.BatchNumber+" 已过期，不能继续被方案使用")
	}
	return nil
}

// EnsureReservable is the checkout gate: plans may only reference lots that
// are unexpired and still hold enough shelf balance for the whole request.
func (b ConsumableBatch) EnsureReservable(now time.Time, quantity int) error {
	if err := b.EnsureNotExpired(now); err != nil {
		return err
	}
	if err := validateMaterialQuantity(quantity); err != nil {
		return err
	}
	if b.Balance <= 0 {
		return contracts.Conflict("batch_exhausted", "耗材批次 "+b.BatchNumber+" 已耗尽，不能继续领用")
	}
	if quantity > b.Balance {
		return contracts.Conflict("batch_insufficient_balance", "耗材批次 "+b.BatchNumber+" 余额不足，不能跨批次余额领用")
	}
	return nil
}

// Reserve moves stock off the shelf during checkout.
func (b *ConsumableBatch) Reserve(quantity int, now time.Time) error {
	if err := validateMaterialQuantity(quantity); err != nil {
		return err
	}
	if quantity > b.Balance {
		return contracts.Conflict("batch_insufficient_balance", "耗材批次 "+b.BatchNumber+" 余额不足")
	}
	b.Balance -= quantity
	b.Version++
	b.UpdatedAt = now.UTC()
	return nil
}

// Restock returns unused stock to the shelf while keeping the batch invariant
// balance <= intake quantity.
func (b *ConsumableBatch) Restock(quantity int, now time.Time) error {
	if err := validateMaterialQuantity(quantity); err != nil {
		return err
	}
	if b.Balance+quantity > b.Quantity {
		return contracts.Conflict("material_return_exceeds_intake", "退料数量超过该批次的可容纳余量")
	}
	b.Balance += quantity
	b.Version++
	b.UpdatedAt = now.UTC()
	return nil
}
