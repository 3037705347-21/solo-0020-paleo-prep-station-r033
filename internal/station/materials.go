package station

import (
	"context"
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

type MaterialUsageDetail struct {
	domain.MaterialUsage
	MaterialKind string `json:"material_kind"`
	BatchNumber  string `json:"batch_number"`
	Unit         string `json:"unit"`
}

type BatchSnapshot struct {
	Batch  domain.MaterialBatch   `json:"batch"`
	Usages []domain.MaterialUsage `json:"usages"`
}

func usageDetails(m *repository.Mutation, usages []domain.MaterialUsage) ([]MaterialUsageDetail, error) {
	details := make([]MaterialUsageDetail, 0, len(usages))
	for _, usage := range usages {
		batch, ok := m.GetBatch(usage.BatchID)
		if !ok {
			return nil, contracts.Internal("missing_usage_batch", "用量记录引用的批次不存在", nil)
		}
		details = append(details, MaterialUsageDetail{
			MaterialUsage: usage,
			MaterialKind:  batch.MaterialKind,
			BatchNumber:   batch.BatchNumber,
			Unit:          batch.Unit,
		})
	}
	return details, nil
}

func (s *Service) ReceiveBatch(
	ctx context.Context,
	request contracts.ReceiveBatchRequest,
) (domain.MaterialBatch, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialBatch{}, err
	}

	var created domain.MaterialBatch
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		batch, err := domain.NewMaterialBatch(
			s.nextID("batch"),
			request.MaterialKind,
			request.BatchNumber,
			request.Unit,
			request.Quantity,
			request.ExpiresAt,
			s.now(),
		)
		if err != nil {
			return err
		}
		if _, exists := m.FindBatchByNumber(batch.BatchNumber); exists {
			return contracts.Conflict("duplicate_batch_number", "批次号已存在，不能重复入库")
		}
		m.PutBatch(batch)
		created = batch
		return nil
	})
	return created, err
}

func (s *Service) ListBatches(ctx context.Context) ([]domain.MaterialBatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.MaterialBatch
	err := s.repo.Read(func(m *repository.Mutation) error {
		values = m.ListBatches()
		return nil
	})
	return values, err
}

func (s *Service) GetBatch(ctx context.Context, batchID string) (BatchSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return BatchSnapshot{}, err
	}
	var snapshot BatchSnapshot
	err := s.repo.Read(func(m *repository.Mutation) error {
		batch, ok := m.GetBatch(batchID)
		if !ok {
			return notFoundBatch(batchID)
		}
		snapshot = BatchSnapshot{
			Batch:  batch,
			Usages: m.ListUsagesForBatch(batchID),
		}
		return nil
	})
	return snapshot, err
}

func (s *Service) IssueMaterial(
	ctx context.Context,
	planID string,
	request contracts.IssueMaterialRequest,
) (domain.MaterialUsage, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialUsage{}, err
	}

	var issued domain.MaterialUsage
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		batchID := strings.TrimSpace(request.BatchID)
		if batchID == "" {
			return contracts.Invalid("required_field", "批次标识不能为空")
		}
		batch, ok := m.GetBatch(batchID)
		if !ok {
			return notFoundBatch(batchID)
		}
		if _, exists := m.FindUsageByPlanAndBatch(plan.ID, batch.ID); exists {
			return contracts.Conflict("duplicate_material_issue", "该方案已领用此批次，不能重复领用")
		}
		usage, err := domain.NewMaterialUsage(
			s.nextID("usage"),
			plan,
			batch,
			request.Quantity,
			s.now(),
		)
		if err != nil {
			return err
		}
		if err := batch.Issue(request.Quantity, s.now()); err != nil {
			return err
		}
		m.PutUsage(usage)
		m.PutBatch(batch)
		issued = usage
		return nil
	})
	return issued, err
}

func (s *Service) ListPlanMaterials(
	ctx context.Context,
	planID string,
) ([]MaterialUsageDetail, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []MaterialUsageDetail
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetPlan(planID); !ok {
			return notFoundPlan(planID)
		}
		details, err := usageDetails(m, m.ListUsagesForPlan(planID))
		if err != nil {
			return err
		}
		values = details
		return nil
	})
	return values, err
}

func (s *Service) ConsumeMaterial(
	ctx context.Context,
	planID string,
	usageID string,
	request contracts.MaterialQuantityRequest,
) (domain.MaterialUsage, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialUsage{}, err
	}

	var updated domain.MaterialUsage
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		usage, ok := m.GetUsage(usageID)
		if !ok || usage.PlanID != plan.ID {
			return notFoundMaterialUsage(usageID)
		}
		if plan.Status != domain.PlanApproved && plan.Status != domain.PlanActive {
			return contracts.Conflict("plan_not_consumable", "只有已审批或进行中的方案可以登记消耗")
		}
		if err := usage.Consume(request.Quantity, s.now()); err != nil {
			return err
		}
		m.PutUsage(usage)
		updated = usage
		return nil
	})
	return updated, err
}

func (s *Service) ReturnMaterial(
	ctx context.Context,
	planID string,
	usageID string,
	request contracts.MaterialQuantityRequest,
) (domain.MaterialUsage, error) {
	if err := ctx.Err(); err != nil {
		return domain.MaterialUsage{}, err
	}

	var updated domain.MaterialUsage
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		usage, ok := m.GetUsage(usageID)
		if !ok || usage.PlanID != plan.ID {
			return notFoundMaterialUsage(usageID)
		}
		if plan.Status != domain.PlanApproved && plan.Status != domain.PlanActive {
			return contracts.Conflict("plan_not_returnable", "只有已审批或进行中的方案可以退料")
		}
		batch, ok := m.GetBatch(usage.BatchID)
		if !ok {
			return notFoundBatch(usage.BatchID)
		}
		if err := usage.Return(request.Quantity, s.now()); err != nil {
			return err
		}
		if err := batch.Restock(request.Quantity, s.now()); err != nil {
			return err
		}
		m.PutUsage(usage)
		m.PutBatch(batch)
		updated = usage
		return nil
	})
	return updated, err
}
