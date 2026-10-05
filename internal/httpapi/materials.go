package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) receiveBatch(w http.ResponseWriter, r *http.Request) {
	var request contracts.ReceiveBatchRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ReceiveBatch(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/materials/batches/"+value.ID)
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) listBatches(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListBatches(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) getBatch(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.GetBatch(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *API) issueMaterial(w http.ResponseWriter, r *http.Request) {
	var request contracts.IssueMaterialRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.IssueMaterial(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/plans/"+r.PathValue("id")+"/materials")
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) listPlanMaterials(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListPlanMaterials(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) consumeMaterial(w http.ResponseWriter, r *http.Request) {
	var request contracts.MaterialQuantityRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ConsumeMaterial(
		r.Context(),
		r.PathValue("id"),
		r.PathValue("usageId"),
		request,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *API) returnMaterial(w http.ResponseWriter, r *http.Request) {
	var request contracts.MaterialQuantityRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ReturnMaterial(
		r.Context(),
		r.PathValue("id"),
		r.PathValue("usageId"),
		request,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
