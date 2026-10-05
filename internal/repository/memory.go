package repository

import (
	"sort"
	"sync"

	"example.com/paleo-prep-station/internal/domain"
)

type state struct {
	specimens map[string]domain.Specimen
	plans     map[string]domain.PrepPlan
	progress  map[string][]domain.ProgressEntry
	releases  map[string]domain.ReleaseRecord
	batches   map[string]domain.ConsumableBatch
	movements []domain.MaterialMovement
	usage     map[string]map[string]domain.PlanMaterialUsage
}

type MemoryRepository struct {
	mu    sync.Mutex
	state state
}

func NewMemory() *MemoryRepository {
	return &MemoryRepository{
		state: state{
			specimens: make(map[string]domain.Specimen),
			plans:     make(map[string]domain.PrepPlan),
			progress:  make(map[string][]domain.ProgressEntry),
			releases:  make(map[string]domain.ReleaseRecord),
			batches:   make(map[string]domain.ConsumableBatch),
			movements: make([]domain.MaterialMovement, 0),
			usage:     make(map[string]map[string]domain.PlanMaterialUsage),
		},
	}
}

func (r *MemoryRepository) Read(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(cloneMutation(r.state))
}

func (r *MemoryRepository) Mutate(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx := cloneMutation(r.state)
	if err := fn(tx); err != nil {
		return err
	}
	r.state = tx.snapshot()
	return nil
}

func cloneMutation(source state) *Mutation {
	copied := state{
		specimens: make(map[string]domain.Specimen, len(source.specimens)),
		plans:     make(map[string]domain.PrepPlan, len(source.plans)),
		progress:  make(map[string][]domain.ProgressEntry, len(source.progress)),
		releases:  make(map[string]domain.ReleaseRecord, len(source.releases)),
		batches:   make(map[string]domain.ConsumableBatch, len(source.batches)),
		movements: append([]domain.MaterialMovement(nil), source.movements...),
		usage:     make(map[string]map[string]domain.PlanMaterialUsage, len(source.usage)),
	}
	for key, value := range source.specimens {
		copied.specimens[key] = value
	}
	for key, value := range source.plans {
		copied.plans[key] = value
	}
	for key, values := range source.progress {
		copied.progress[key] = append([]domain.ProgressEntry(nil), values...)
	}
	for key, value := range source.releases {
		copied.releases[key] = value
	}
	for key, value := range source.batches {
		copied.batches[key] = value
	}
	for planID, lines := range source.usage {
		copiedLines := make(map[string]domain.PlanMaterialUsage, len(lines))
		for batchID, line := range lines {
			copiedLines[batchID] = line
		}
		copied.usage[planID] = copiedLines
	}
	return &Mutation{state: copied}
}

type Mutation struct {
	state state
}

func (m *Mutation) snapshot() state {
	return m.state
}

func (m *Mutation) GetSpecimen(id string) (domain.Specimen, bool) {
	value, ok := m.state.specimens[id]
	return value, ok
}

func (m *Mutation) FindSpecimenByCatalog(catalogNumber string) (domain.Specimen, bool) {
	for _, value := range m.state.specimens {
		if value.CatalogNumber == catalogNumber {
			return value, true
		}
	}
	return domain.Specimen{}, false
}

func (m *Mutation) ListSpecimens() []domain.Specimen {
	values := make([]domain.Specimen, 0, len(m.state.specimens))
	for _, value := range m.state.specimens {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutSpecimen(value domain.Specimen) {
	m.state.specimens[value.ID] = value
}

func (m *Mutation) GetPlan(id string) (domain.PrepPlan, bool) {
	value, ok := m.state.plans[id]
	return value, ok
}

func (m *Mutation) ListPlansForSpecimen(specimenID string) []domain.PrepPlan {
	values := make([]domain.PrepPlan, 0)
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutPlan(value domain.PrepPlan) {
	m.state.plans[value.ID] = value
}

func (m *Mutation) HasOpenPlan(specimenID string) bool {
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID && value.IsOpen() {
			return true
		}
	}
	return false
}

func (m *Mutation) ProgressForPlan(planID string) []domain.ProgressEntry {
	return append([]domain.ProgressEntry(nil), m.state.progress[planID]...)
}

func (m *Mutation) AppendProgress(value domain.ProgressEntry) {
	m.state.progress[value.PlanID] = append(m.state.progress[value.PlanID], value)
}

func (m *Mutation) ReleaseForSpecimen(specimenID string) (domain.ReleaseRecord, bool) {
	for _, value := range m.state.releases {
		if value.SpecimenID == specimenID {
			return value, true
		}
	}
	return domain.ReleaseRecord{}, false
}

func (m *Mutation) PutRelease(value domain.ReleaseRecord) {
	m.state.releases[value.ID] = value
}

func (m *Mutation) GetBatch(id string) (domain.ConsumableBatch, bool) {
	value, ok := m.state.batches[id]
	return value, ok
}

func (m *Mutation) FindBatchByNumber(batchNumber string) (domain.ConsumableBatch, bool) {
	for _, value := range m.state.batches {
		if value.BatchNumber == batchNumber {
			return value, true
		}
	}
	return domain.ConsumableBatch{}, false
}

func (m *Mutation) ListBatches() []domain.ConsumableBatch {
	values := make([]domain.ConsumableBatch, 0, len(m.state.batches))
	for _, value := range m.state.batches {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].ReceivedAt.Equal(values[j].ReceivedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].ReceivedAt.Before(values[j].ReceivedAt)
	})
	return values
}

func (m *Mutation) PutBatch(value domain.ConsumableBatch) {
	m.state.batches[value.ID] = value
}

// AppendMovement records one immutable balance fact in ledger order.
func (m *Mutation) AppendMovement(value domain.MaterialMovement) {
	m.state.movements = append(m.state.movements, value)
}

func (m *Mutation) MovementsForBatch(batchID string) []domain.MaterialMovement {
	values := make([]domain.MaterialMovement, 0)
	for _, value := range m.state.movements {
		if value.BatchID == batchID {
			values = append(values, value)
		}
	}
	return values
}

// MaterialsForPlan returns the plan's usage lines sorted by first reference.
func (m *Mutation) MaterialsForPlan(planID string) []domain.PlanMaterialUsage {
	lines := m.state.usage[planID]
	values := make([]domain.PlanMaterialUsage, 0, len(lines))
	for _, value := range lines {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].BatchID < values[j].BatchID
	})
	return values
}

func (m *Mutation) GetPlanMaterial(planID, batchID string) (domain.PlanMaterialUsage, bool) {
	lines, ok := m.state.usage[planID]
	if !ok {
		return domain.PlanMaterialUsage{}, false
	}
	value, ok := lines[batchID]
	return value, ok
}

func (m *Mutation) PutPlanMaterial(value domain.PlanMaterialUsage) {
	lines, ok := m.state.usage[value.PlanID]
	if !ok {
		lines = make(map[string]domain.PlanMaterialUsage)
		m.state.usage[value.PlanID] = lines
	}
	lines[value.BatchID] = value
}
