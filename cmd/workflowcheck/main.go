package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

type specimenView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
}

type batchView struct {
	ID      string `json:"id"`
	Balance int    `json:"balance"`
	Status  string `json:"status"`
}

type batchLedgerView struct {
	Batch     batchView `json:"batch"`
	Movements []struct {
		Type         string `json:"type"`
		BalanceAfter int    `json:"balance_after"`
	} `json:"movements"`
}

type materialUsageView struct {
	BatchID    string `json:"batch_id"`
	CheckedOut int    `json:"checked_out"`
	Consumed   int    `json:"consumed"`
	Returned   int    `json:"returned"`
}

type planMaterialsSnapshot struct {
	Plan      planView            `json:"plan"`
	Materials []materialUsageView `json:"materials"`
	Progress  []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
}

func main() {
	workflow := flag.String("workflow", "", "workflow id to check")
	flag.Parse()

	if err := run(*workflow); err != nil {
		fmt.Fprintln(os.Stderr, "workflow check failed:", err)
		os.Exit(1)
	}
	fmt.Printf("workflow check passed: %s\n", *workflow)
}

func run(workflow string) error {
	handler := httpapi.NewRouter(station.New(repository.NewMemory()))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	switch workflow {
	case "register-specimen":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		if specimen.Status != "registered" {
			return fmt.Errorf("expected registered status, got %s", specimen.Status)
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.ID != specimen.ID {
			return errors.New("registered specimen cannot be read back")
		}
		return nil

	case "approve-prep-plan":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		plan, err := createPlan(client, server.URL, specimen.ID)
		if err != nil {
			return err
		}
		var approved planView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/plans/"+plan.ID+"/approval",
			contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
			http.StatusOK,
			&approved,
		); err != nil {
			return err
		}
		if approved.Status != "approved" {
			return fmt.Errorf("expected approved plan, got %s", approved.Status)
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "preparing" {
			return fmt.Errorf("expected preparing specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "log-preparation":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 35, "矩阵剥离"); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var snapshot planSnapshot
		if err := requestJSON(client, http.MethodGet, server.URL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
			return err
		}
		if snapshot.Plan.Status != "completed" || len(snapshot.Progress) != 2 {
			return errors.New("plan did not complete with exactly two progress entries")
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "stabilized" {
			return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "release-for-study":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var release releaseView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/specimens/"+specimen.ID+"/release",
			contracts.ReleaseRequest{
				Purpose:    "比较解剖研究",
				Recipient:  "地层研究组",
				ApprovedBy: "keeper-zhou",
			},
			http.StatusCreated,
			&release,
		); err != nil {
			return err
		}
		if release.SpecimenID != specimen.ID {
			return errors.New("release record references the wrong specimen")
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.Status != "available" {
			return fmt.Errorf("expected available specimen, got %s", fetched.Status)
		}
		return nil

	case "track-material-batches":
		return checkMaterialBatches(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func checkMaterialBatches(client *http.Client, baseURL string) error {
	_, plan, err := completedPlanSetup(client, baseURL)
	if err != nil {
		return err
	}

	// Intake two lots: a live consolidant and one that expires immediately.
	batch, err := intakeBatch(client, baseURL, contracts.IntakeBatchRequest{
		Kind:        "consolidant",
		Name:        "丙烯酸加固液",
		BatchNumber: "LOT-2026-A",
		Unit:        "ml",
		Quantity:    100,
		ReceivedBy:  "keeper-zhou",
		ExpiresAt:   time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	if batch.Balance != 100 || batch.Status != "available" {
		return fmt.Errorf("fresh batch balance/status wrong: %+v", batch)
	}
	expiredBatch, err := intakeBatch(client, baseURL, contracts.IntakeBatchRequest{
		Kind:        "embedding",
		Name:        "石膏包埋粉",
		BatchNumber: "LOT-2026-X",
		Unit:        "g",
		Quantity:    50,
		ReceivedBy:  "keeper-zhou",
		ExpiresAt:   time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	})
	if err == nil {
		return fmt.Errorf("intake of an already-expired batch must fail, got %+v", expiredBatch)
	}

	// Expired lots can never be referenced, even when balance exists.
	shortBatch, err := intakeBatch(client, baseURL, contracts.IntakeBatchRequest{
		Kind:        "adhesive",
		Name:        "快干胶结剂",
		BatchNumber: "LOT-2026-S",
		Unit:        "ml",
		Quantity:    20,
		ReceivedBy:  "keeper-zhou",
		ExpiresAt:   time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	if err := checkoutMaterial(client, baseURL, plan.ID, contracts.MaterialCheckoutRequest{
		BatchID: shortBatch.ID, Quantity: 5, Operator: "preparator-chen",
	}); err != nil {
		return fmt.Errorf("first short-batch checkout failed: %w", err)
	}
	time.Sleep(2100 * time.Millisecond)
	if err := checkoutMaterial(client, baseURL, plan.ID, contracts.MaterialCheckoutRequest{
		BatchID: shortBatch.ID, Quantity: 5, Operator: "preparator-chen",
	}); err == nil {
		return errors.New("checkout from an expired batch must fail")
	}
	// Held stock of an expired lot must be returned to allow settlement.
	if err := returnMaterial(client, baseURL, plan.ID, contracts.MaterialReturnRequest{
		BatchID: shortBatch.ID, Quantity: 5, Operator: "preparator-chen",
	}); err != nil {
		return fmt.Errorf("returning expired held stock failed: %w", err)
	}

	// Checkout 60 of 100; duplicate checkout while held must fail.
	if err := checkoutMaterial(client, baseURL, plan.ID, contracts.MaterialCheckoutRequest{
		BatchID: batch.ID, Quantity: 60, Operator: "preparator-chen",
	}); err != nil {
		return err
	}
	if err := checkoutMaterial(client, baseURL, plan.ID, contracts.MaterialCheckoutRequest{
		BatchID: batch.ID, Quantity: 10, Operator: "preparator-chen",
	}); err == nil {
		return errors.New("duplicate checkout while stock is held must fail")
	}

	// Concurrent over-subscription by a second plan must leave no half state.
	_, plan2, err := completedPlanSetupWithCatalog(client, baseURL, "PPS-2026-002")
	if err != nil {
		return err
	}
	if err := checkoutMaterial(client, baseURL, plan2.ID, contracts.MaterialCheckoutRequest{
		BatchID: batch.ID, Quantity: 50, Operator: "preparator-chen",
	}); err == nil {
		return errors.New("cross-plan over-subscription must fail (only 40 remain)")
	}
	if err := checkoutMaterial(client, baseURL, plan2.ID, contracts.MaterialCheckoutRequest{
		BatchID: batch.ID, Quantity: 40, Operator: "preparator-chen",
	}); err != nil {
		return fmt.Errorf("checkout of exact remaining balance failed: %w", err)
	}
	// Batch is now exhausted: further reference and consumption must fail.
	if err := checkoutMaterial(client, baseURL, plan2.ID, contracts.MaterialCheckoutRequest{
		BatchID: batch.ID, Quantity: 1, Operator: "preparator-chen",
	}); err == nil {
		return errors.New("checkout from an exhausted batch must fail")
	}

	// Consuming more than held must fail; then settle both plans.
	if err := consumeMaterial(client, baseURL, plan.ID, contracts.MaterialConsumptionRequest{
		BatchID: batch.ID, Quantity: 61, Operator: "preparator-chen",
	}); err == nil {
		return errors.New("consumption above held quantity must fail")
	}
	if err := consumeMaterial(client, baseURL, plan.ID, contracts.MaterialConsumptionRequest{
		BatchID: batch.ID, Quantity: 45, Operator: "preparator-chen",
	}); err != nil {
		return err
	}
	if err := returnMaterial(client, baseURL, plan.ID, contracts.MaterialReturnRequest{
		BatchID: batch.ID, Quantity: 15, Operator: "preparator-chen",
	}); err != nil {
		return err
	}
	if err := consumeMaterial(client, baseURL, plan2.ID, contracts.MaterialConsumptionRequest{
		BatchID: batch.ID, Quantity: 30, Operator: "preparator-chen",
	}); err != nil {
		return err
	}

	// Ledger and batch balance must agree: 100 - 60 - 40 + 15 = 15.
	var ledger batchLedgerView
	if err := requestJSON(client, http.MethodGet, baseURL+"/material-batches/"+batch.ID, nil, http.StatusOK, &ledger); err != nil {
		return err
	}
	if ledger.Batch.Balance != 15 {
		return fmt.Errorf("expected batch balance 15, got %d", ledger.Batch.Balance)
	}
	last := ledger.Movements[len(ledger.Movements)-1]
	if last.BalanceAfter != 15 {
		return fmt.Errorf("last ledger balance_after wrong: %d", last.BalanceAfter)
	}

	// First plan cannot complete while nothing is held but materials settled:
	// it can complete; verify usage facts are returned with the plan snapshot.
	if err := recordProgress(client, baseURL, plan.ID, 100, "表面加固"); err != nil {
		return err
	}
	var snapshot planMaterialsSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
		return err
	}
	if snapshot.Plan.Status != "completed" {
		return fmt.Errorf("expected completed plan, got %s", snapshot.Plan.Status)
	}
	var line *materialUsageView
	for i := range snapshot.Materials {
		if snapshot.Materials[i].BatchID == batch.ID {
			line = &snapshot.Materials[i]
		}
	}
	if line == nil || line.CheckedOut != 60 || line.Consumed != 45 || line.Returned != 15 {
		return fmt.Errorf("plan usage facts inconsistent: %+v", snapshot.Materials)
	}

	// Completed plans must reject further consumption (no post-completion drift).
	if err := consumeMaterial(client, baseURL, plan.ID, contracts.MaterialConsumptionRequest{
		BatchID: batch.ID, Quantity: 1, Operator: "preparator-chen",
	}); err == nil {
		return errors.New("consumption on a completed plan must fail")
	}

	// Unsettled material blocks plan completion and leaves no half-finished state.
	if err := recordProgress(client, baseURL, plan2.ID, 80, "基质剥离"); err != nil {
		return err
	}
	if err := recordProgress(client, baseURL, plan2.ID, 100, "收尾"); err == nil {
		return errors.New("completion with unsettled checkout must fail")
	}
	var open planMaterialsSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan2.ID, nil, http.StatusOK, &open); err != nil {
		return err
	}
	if open.Plan.Status != "active" || len(open.Progress) != 1 {
		return fmt.Errorf("failed completion left a half-finished state: %+v", open.Plan)
	}
	return nil
}

func registerSpecimen(client *http.Client, baseURL string) (specimenView, error) {
	return registerSpecimenWithCatalog(client, baseURL, "PPS-2026-001")
}

func registerSpecimenWithCatalog(client *http.Client, baseURL, catalog string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: catalog,
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func createPlan(client *http.Client, baseURL, specimenID string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露主要关节面",
			Method:         "手工显微剥离",
			Adhesive:       "可逆树脂",
			TargetExposure: 80,
		},
		http.StatusCreated,
		&plan,
	)
	return plan, err
}

func completedPlanSetup(client *http.Client, baseURL string) (specimenView, planView, error) {
	return completedPlanSetupWithCatalog(client, baseURL, "PPS-2026-001")
}

func completedPlanSetupWithCatalog(client *http.Client, baseURL, catalog string) (specimenView, planView, error) {
	specimen, err := registerSpecimenWithCatalog(client, baseURL, catalog)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	var approved planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		&approved,
	); err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, approved, nil
}

func recordProgress(client *http.Client, baseURL, planID string, percent int, stage string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		contracts.ProgressRequest{
			Stage:      stage,
			Percent:    percent,
			Note:       "本地链路检查",
			RecordedBy: "preparator-chen",
		},
		http.StatusCreated,
		&ignored,
	)
}

func intakeBatch(client *http.Client, baseURL string, request contracts.IntakeBatchRequest) (batchView, error) {
	var batch batchView
	err := requestJSON(client, http.MethodPost, baseURL+"/material-batches", request, http.StatusCreated, &batch)
	return batch, err
}

func checkoutMaterial(client *http.Client, baseURL, planID string, request contracts.MaterialCheckoutRequest) error {
	var ignored struct{}
	return requestJSON(client, http.MethodPost, baseURL+"/plans/"+planID+"/material-checkouts", request, http.StatusCreated, &ignored)
}

func consumeMaterial(client *http.Client, baseURL, planID string, request contracts.MaterialConsumptionRequest) error {
	var ignored struct{}
	return requestJSON(client, http.MethodPost, baseURL+"/plans/"+planID+"/material-consumptions", request, http.StatusCreated, &ignored)
}

func returnMaterial(client *http.Client, baseURL, planID string, request contracts.MaterialReturnRequest) error {
	var ignored struct{}
	return requestJSON(client, http.MethodPost, baseURL+"/plans/"+planID+"/material-returns", request, http.StatusCreated, &ignored)
}

func requestJSON(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	target any,
) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("request %s %s returned %d: %s", method, url, response.StatusCode, string(raw))
	}
	if target == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return nil
}
