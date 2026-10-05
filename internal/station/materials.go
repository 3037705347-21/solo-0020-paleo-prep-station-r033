package station

import (
	"context"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// BatchSnapshot pairs a batch with its derived availability status.
type BatchSnapshot struct {
	domain.ConsumableBatch
	Status domain.BatchStatus `json:"status"`
}

// BatchLedger is a batch plus the full movement history that explains Balance.
type BatchLedger struct {
	Batch     BatchSnapshot             `json:"batch"`
	Movements []domain.MaterialMovement `json:"movements"`
}

func (s *Service) IntakeBatch(
	ctx context.Context,
	request contracts.IntakeBatchRequest,
) (BatchSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return BatchSnapshot{}, err
	}
	kind, err := domain.ParseMaterialKind(request.Kind)
	if err != nil {
		return BatchSnapshot{}, err
	}
	expiresAt, err := parseExpiresAt(request.ExpiresAt)
	if err != nil {
		return BatchSnapshot{}, err
	}

	var created domain.ConsumableBatch
	err = s.repo.Mutate(func(m *repository.Mutation) error {
		batch, err := domain.NewConsumableBatch(
			s.nextID("batch"),
			kind,
			request.Name,
			request.BatchNumber,
			request.Unit,
			request.Quantity,
			request.ReceivedBy,
			expiresAt,
			s.now(),
		)
		if err != nil {
			return err
		}
		if _, exists := m.FindBatchByNumber(batch.BatchNumber); exists {
			return contracts.Conflict("duplicate_batch_number", "批次号已存在，不能覆盖原批次")
		}
		movement, err := domain.NewMaterialMovement(
			s.nextID("movement"),
			batch,
			domain.MovementIntake,
			batch.Quantity,
			batch.Balance,
			"",
			batch.ReceivedBy,
			request.Note,
			s.now(),
		)
		if err != nil {
			return err
		}
		m.PutBatch(batch)
		m.AppendMovement(movement)
		created = batch
		return nil
	})
	if err != nil {
		return BatchSnapshot{}, err
	}
	return s.batchSnapshot(created), nil
}

func (s *Service) ListBatches(ctx context.Context) ([]BatchSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []BatchSnapshot
	err := s.repo.Read(func(m *repository.Mutation) error {
		for _, batch := range m.ListBatches() {
			values = append(values, s.batchSnapshot(batch))
		}
		return nil
	})
	return values, err
}

func (s *Service) GetBatch(ctx context.Context, batchID string) (BatchLedger, error) {
	if err := ctx.Err(); err != nil {
		return BatchLedger{}, err
	}
	var ledger BatchLedger
	err := s.repo.Read(func(m *repository.Mutation) error {
		batch, ok := m.GetBatch(batchID)
		if !ok {
			return notFoundBatch(batchID)
		}
		ledger = BatchLedger{
			Batch:     s.batchSnapshot(batch),
			Movements: m.MovementsForBatch(batchID),
		}
		return nil
	})
	return ledger, err
}

func (s *Service) CheckoutMaterial(
	ctx context.Context,
	planID string,
	request contracts.MaterialCheckoutRequest,
) (domain.MaterialMovement, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialMovement{}, err
	}

	var created domain.MaterialMovement
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, batch, err := s.loadCheckoutTargets(m, planID, request.BatchID)
		if err != nil {
			return err
		}
		if err := batch.EnsureReservable(s.now(), request.Quantity); err != nil {
			return err
		}
		usage, exists := m.GetPlanMaterial(plan.ID, batch.ID)
		if !exists {
			usage = domain.NewPlanUsageLine(plan.ID, batch, s.now())
		}
		if err := usage.Checkout(request.Quantity, s.now()); err != nil {
			return err
		}
		if err := batch.Reserve(request.Quantity, s.now()); err != nil {
			return err
		}
		movement, err := domain.NewMaterialMovement(
			s.nextID("movement"),
			batch,
			domain.MovementCheckout,
			request.Quantity,
			batch.Balance,
			plan.ID,
			request.Operator,
			request.Note,
			s.now(),
		)
		if err != nil {
			return err
		}
		m.PutBatch(batch)
		m.PutPlanMaterial(usage)
		m.AppendMovement(movement)
		created = movement
		return nil
	})
	return created, err
}

func (s *Service) ConsumeMaterial(
	ctx context.Context,
	planID string,
	request contracts.MaterialConsumptionRequest,
) (domain.MaterialMovement, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialMovement{}, err
	}

	var created domain.MaterialMovement
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, batch, usage, err := s.loadHeldMaterial(m, planID, request.BatchID)
		if err != nil {
			return err
		}
		// An expired lot must never be consumed even if it was checked out earlier.
		if err := batch.EnsureNotExpired(s.now()); err != nil {
			return err
		}
		if err := usage.Consume(request.Quantity, s.now()); err != nil {
			return err
		}
		// Consumption does not move shelf balance again; checkout already did.
		movement, err := domain.NewMaterialMovement(
			s.nextID("movement"),
			batch,
			domain.MovementConsumption,
			request.Quantity,
			batch.Balance,
			plan.ID,
			request.Operator,
			request.Note,
			s.now(),
		)
		if err != nil {
			return err
		}
		m.PutPlanMaterial(usage)
		m.AppendMovement(movement)
		created = movement
		return nil
	})
	return created, err
}

func (s *Service) ReturnMaterial(
	ctx context.Context,
	planID string,
	request contracts.MaterialReturnRequest,
) (domain.MaterialMovement, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialMovement{}, err
	}

	var created domain.MaterialMovement
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, batch, usage, err := s.loadHeldMaterial(m, planID, request.BatchID)
		if err != nil {
			return err
		}
		if err := usage.Return(request.Quantity, s.now()); err != nil {
			return err
		}
		if err := batch.Restock(request.Quantity, s.now()); err != nil {
			return err
		}
		movement, err := domain.NewMaterialMovement(
			s.nextID("movement"),
			batch,
			domain.MovementReturn,
			request.Quantity,
			batch.Balance,
			plan.ID,
			request.Operator,
			request.Note,
			s.now(),
		)
		if err != nil {
			return err
		}
		m.PutBatch(batch)
		m.PutPlanMaterial(usage)
		m.AppendMovement(movement)
		created = movement
		return nil
	})
	return created, err
}

func (s *Service) ListPlanMaterials(
	ctx context.Context,
	planID string,
) ([]domain.PlanMaterialUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.PlanMaterialUsage
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetPlan(planID); !ok {
			return notFoundPlan(planID)
		}
		values = m.MaterialsForPlan(planID)
		return nil
	})
	return values, err
}

func (s *Service) batchSnapshot(batch domain.ConsumableBatch) BatchSnapshot {
	return BatchSnapshot{
		ConsumableBatch: batch,
		Status:          batch.Status(s.now()),
	}
}

// loadCheckoutTargets enforces that only approved or active plans can
// reference a consumable batch.
func (s *Service) loadCheckoutTargets(
	m *repository.Mutation,
	planID, batchID string,
) (domain.PrepPlan, domain.ConsumableBatch, error) {
	plan, ok := m.GetPlan(planID)
	if !ok {
		return domain.PrepPlan{}, domain.ConsumableBatch{}, notFoundPlan(planID)
	}
	if plan.Status != domain.PlanApproved && plan.Status != domain.PlanActive {
		return domain.PrepPlan{}, domain.ConsumableBatch{}, contracts.Conflict(
			"plan_not_checkoutable",
			"只有已审批或进行中的方案可以领用耗材",
		)
	}
	batch, ok := m.GetBatch(batchID)
	if !ok {
		return domain.PrepPlan{}, domain.ConsumableBatch{}, notFoundBatch(batchID)
	}
	return plan, batch, nil
}

// loadHeldMaterial loads the plan/batch pair plus its open usage line for
// consumption and return.
func (s *Service) loadHeldMaterial(
	m *repository.Mutation,
	planID, batchID string,
) (domain.PrepPlan, domain.ConsumableBatch, domain.PlanMaterialUsage, error) {
	plan, ok := m.GetPlan(planID)
	if !ok {
		return domain.PrepPlan{}, domain.ConsumableBatch{}, domain.PlanMaterialUsage{}, notFoundPlan(planID)
	}
	if plan.Status != domain.PlanApproved && plan.Status != domain.PlanActive {
		return domain.PrepPlan{}, domain.ConsumableBatch{}, domain.PlanMaterialUsage{}, contracts.Conflict(
			"plan_not_material_active",
			"只有已审批或进行中的方案可以登记耗材消耗或退料",
		)
	}
	batch, ok := m.GetBatch(batchID)
	if !ok {
		return domain.PrepPlan{}, domain.ConsumableBatch{}, domain.PlanMaterialUsage{}, notFoundBatch(batchID)
	}
	usage, exists := m.GetPlanMaterial(planID, batchID)
	if !exists {
		return domain.PrepPlan{}, domain.ConsumableBatch{}, domain.PlanMaterialUsage{}, notFoundMaterialLine(planID, batchID)
	}
	return plan, batch, usage, nil
}

func parseExpiresAt(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, contracts.Invalid("invalid_expires_at", "过期时间必须是 RFC3339 格式")
	}
	return parsed, nil
}
