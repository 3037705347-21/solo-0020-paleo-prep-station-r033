package contracts

type RegisterSpecimenRequest struct {
	CatalogNumber string `json:"catalog_number"`
	FieldLabel    string `json:"field_label"`
	TaxonHint     string `json:"taxon_hint"`
	MatrixKind    string `json:"matrix_kind"`
	JacketNotes   string `json:"jacket_notes"`
}

type CreatePlanRequest struct {
	Objective      string `json:"objective"`
	Method         string `json:"method"`
	Adhesive       string `json:"adhesive"`
	TargetExposure int    `json:"target_exposure"`
}

type ApprovalRequest struct {
	ApprovedBy string `json:"approved_by"`
}

type ProgressRequest struct {
	Stage      string `json:"stage"`
	Percent    int    `json:"percent"`
	Note       string `json:"note"`
	RecordedBy string `json:"recorded_by"`
}

type ReleaseRequest struct {
	Purpose    string `json:"purpose"`
	Recipient  string `json:"recipient"`
	ApprovedBy string `json:"approved_by"`
}

// IntakeBatchRequest registers one consumable lot on the bench.
type IntakeBatchRequest struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	BatchNumber string `json:"batch_number"`
	Unit        string `json:"unit"`
	Quantity    int    `json:"quantity"`
	ReceivedBy  string `json:"received_by"`
	ExpiresAt   string `json:"expires_at"`
	Note        string `json:"note"`
}

// MaterialCheckoutRequest reserves stock from one batch for one plan.
type MaterialCheckoutRequest struct {
	BatchID  string `json:"batch_id"`
	Quantity int    `json:"quantity"`
	Operator string `json:"operator"`
	Note     string `json:"note"`
}

// MaterialConsumptionRequest records actual use of held stock.
type MaterialConsumptionRequest struct {
	BatchID  string `json:"batch_id"`
	Quantity int    `json:"quantity"`
	Operator string `json:"operator"`
	Note     string `json:"note"`
}

// MaterialReturnRequest gives unused held stock back to the shelf.
type MaterialReturnRequest struct {
	BatchID  string `json:"batch_id"`
	Quantity int    `json:"quantity"`
	Operator string `json:"operator"`
	Note     string `json:"note"`
}
