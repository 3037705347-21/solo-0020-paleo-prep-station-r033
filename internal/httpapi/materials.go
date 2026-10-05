package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) intakeBatch(w http.ResponseWriter, r *http.Request) {
	var request contracts.IntakeBatchRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.IntakeBatch(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/material-batches/"+value.ID)
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

func (api *API) checkoutMaterial(w http.ResponseWriter, r *http.Request) {
	var request contracts.MaterialCheckoutRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.CheckoutMaterial(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) consumeMaterial(w http.ResponseWriter, r *http.Request) {
	var request contracts.MaterialConsumptionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ConsumeMaterial(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) returnMaterial(w http.ResponseWriter, r *http.Request) {
	var request contracts.MaterialReturnRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ReturnMaterial(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
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
