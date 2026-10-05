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
	Materials []materialUsageView `json:"materials"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
}

type batchView struct {
	ID          string `json:"id"`
	BatchNumber string `json:"batch_number"`
	Received    int    `json:"received"`
	Remaining   int    `json:"remaining"`
}

type batchSnapshotView struct {
	Batch  batchView           `json:"batch"`
	Usages []materialUsageView `json:"usages"`
}

type materialUsageView struct {
	ID       string `json:"id"`
	PlanID   string `json:"plan_id"`
	BatchID  string `json:"batch_id"`
	Issued   int    `json:"issued"`
	Consumed int    `json:"consumed"`
	Returned int    `json:"returned"`
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
	case "material-usage":
		return checkMaterialUsage(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
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
	return approvedPlanSetup(client, baseURL, "PPS-2026-001")
}

func approvedPlanSetup(client *http.Client, baseURL, catalog string) (specimenView, planView, error) {
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
	return recordProgressExpect(client, baseURL, planID, percent, stage, http.StatusCreated)
}

func recordProgressExpect(client *http.Client, baseURL, planID string, percent int, stage string, wantStatus int) error {
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
		wantStatus,
		&ignored,
	)
}

func checkMaterialUsage(client *http.Client, baseURL string) error {
	future := time.Now().Add(30 * 24 * time.Hour).UTC()
	past := time.Now().Add(-time.Hour).UTC()

	batchA, err := receiveBatch(client, baseURL, "加固液", "CONS-2026-001", "ml", 100, future, http.StatusCreated)
	if err != nil {
		return err
	}
	if batchA.Received != 100 || batchA.Remaining != 100 {
		return fmt.Errorf("fresh batch should hold 100, got %+v", batchA)
	}
	if _, err := receiveBatch(client, baseURL, "加固液", "CONS-2026-001", "ml", 5, future, http.StatusConflict); err != nil {
		return errors.New("duplicate batch number must be rejected")
	}
	batchB, err := receiveBatch(client, baseURL, "胶结剂", "ADH-2026-002", "ml", 10, future, http.StatusCreated)
	if err != nil {
		return err
	}
	expired, err := receiveBatch(client, baseURL, "包埋材料", "EMB-2020-099", "g", 5, past, http.StatusCreated)
	if err != nil {
		return err
	}
	var batchList struct {
		Items []batchView `json:"items"`
	}
	if err := requestJSON(client, http.MethodGet, baseURL+"/materials/batches", nil, http.StatusOK, &batchList); err != nil {
		return err
	}
	if len(batchList.Items) != 3 {
		return fmt.Errorf("expected 3 batches, got %d", len(batchList.Items))
	}

	specimen, plan, err := approvedPlanSetup(client, baseURL, "PPS-2026-101")
	if err != nil {
		return err
	}

	usageA, err := issueMaterial(client, baseURL, plan.ID, batchA.ID, 40, http.StatusCreated)
	if err != nil {
		return err
	}
	if usageA.Issued != 40 || usageA.BatchID != batchA.ID || usageA.PlanID != plan.ID {
		return fmt.Errorf("unexpected usage record %+v", usageA)
	}
	if _, err := issueMaterial(client, baseURL, plan.ID, batchA.ID, 1, http.StatusConflict); err != nil {
		return errors.New("duplicate issue for the same plan and batch must fail")
	}
	if _, err := issueMaterial(client, baseURL, plan.ID, expired.ID, 1, http.StatusConflict); err != nil {
		return errors.New("expired batch must not be referenced by a plan")
	}
	batchA, err = fetchBatch(client, baseURL, batchA.ID, 1)
	if err != nil {
		return err
	}
	if batchA.Remaining != 60 {
		return fmt.Errorf("expected remaining 60 after issue, got %d", batchA.Remaining)
	}

	if _, err := consumeMaterial(client, baseURL, plan.ID, usageA.ID, 10, http.StatusOK); err != nil {
		return err
	}
	usageA, err = returnMaterial(client, baseURL, plan.ID, usageA.ID, 10, http.StatusOK)
	if err != nil {
		return err
	}
	if usageA.Consumed != 10 || usageA.Returned != 10 {
		return fmt.Errorf("unexpected usage after consume and return %+v", usageA)
	}
	batchA, err = fetchBatch(client, baseURL, batchA.ID, 1)
	if err != nil {
		return err
	}
	if batchA.Remaining != 70 {
		return fmt.Errorf("expected remaining 70 after return, got %d", batchA.Remaining)
	}

	if err := concurrentConsume(client, baseURL, plan.ID, usageA.ID, 20); err != nil {
		return err
	}
	usageA, err = fetchPlanUsage(client, baseURL, plan.ID, usageA.ID)
	if err != nil {
		return err
	}
	if usageA.Issued != 40 || usageA.Consumed != 30 || usageA.Returned != 10 {
		return fmt.Errorf("concurrent consume left inconsistent usage %+v", usageA)
	}
	if _, err := consumeMaterial(client, baseURL, plan.ID, usageA.ID, 1, http.StatusConflict); err != nil {
		return errors.New("consume beyond held quantity must fail")
	}

	usageB, err := issueMaterial(client, baseURL, plan.ID, batchB.ID, 10, http.StatusCreated)
	if err != nil {
		return err
	}
	batchB, err = fetchBatch(client, baseURL, batchB.ID, 1)
	if err != nil {
		return err
	}
	if batchB.Remaining != 0 {
		return fmt.Errorf("expected depleted batch, got %d", batchB.Remaining)
	}

	if err := recordProgressExpect(client, baseURL, plan.ID, 100, "表面加固", http.StatusConflict); err != nil {
		return errors.New("plan with unsettled material usage must not complete")
	}
	if _, err := returnMaterial(client, baseURL, plan.ID, usageB.ID, 10, http.StatusOK); err != nil {
		return err
	}
	if err := recordProgressExpect(client, baseURL, plan.ID, 100, "表面加固", http.StatusCreated); err != nil {
		return err
	}

	var snapshot planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
		return err
	}
	if snapshot.Plan.Status != "completed" || len(snapshot.Materials) != 2 {
		return errors.New("plan should complete with exactly two material usage records")
	}
	for _, usage := range snapshot.Materials {
		if usage.Issued != usage.Consumed+usage.Returned {
			return fmt.Errorf("completed plan carries unsettled usage %+v", usage)
		}
	}
	var fetchedSpecimen specimenView
	if err := requestJSON(client, http.MethodGet, baseURL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
		return err
	}
	if fetchedSpecimen.Status != "stabilized" {
		return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
	}

	batchA, err = fetchBatch(client, baseURL, batchA.ID, 1)
	if err != nil {
		return err
	}
	if batchA.Remaining != 70 {
		return fmt.Errorf("batch balance inconsistent with usage: got %d want 70", batchA.Remaining)
	}
	batchB, err = fetchBatch(client, baseURL, batchB.ID, 1)
	if err != nil {
		return err
	}
	if batchB.Remaining != 10 {
		return fmt.Errorf("batch balance inconsistent with usage: got %d want 10", batchB.Remaining)
	}

	_, plan2, err := approvedPlanSetup(client, baseURL, "PPS-2026-102")
	if err != nil {
		return err
	}
	if _, err := issueMaterial(client, baseURL, plan2.ID, batchB.ID, 10, http.StatusCreated); err != nil {
		return err
	}
	_, plan3, err := approvedPlanSetup(client, baseURL, "PPS-2026-103")
	if err != nil {
		return err
	}
	if _, err := issueMaterial(client, baseURL, plan3.ID, batchB.ID, 1, http.StatusConflict); err != nil {
		return errors.New("depleted batch must reject further issues")
	}
	if _, err := issueMaterial(client, baseURL, plan3.ID, batchA.ID, 71, http.StatusConflict); err != nil {
		return errors.New("issue beyond batch balance must fail")
	}
	return nil
}

func receiveBatch(
	client *http.Client,
	baseURL string,
	kind string,
	number string,
	unit string,
	quantity int,
	expiresAt time.Time,
	wantStatus int,
) (batchView, error) {
	var batch batchView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/materials/batches",
		contracts.ReceiveBatchRequest{
			MaterialKind: kind,
			BatchNumber:  number,
			Unit:         unit,
			Quantity:     quantity,
			ExpiresAt:    expiresAt,
		},
		wantStatus,
		&batch,
	)
	return batch, err
}

func fetchBatch(client *http.Client, baseURL, batchID string, wantUsages int) (batchView, error) {
	var snapshot batchSnapshotView
	if err := requestJSON(
		client,
		http.MethodGet,
		baseURL+"/materials/batches/"+batchID,
		nil,
		http.StatusOK,
		&snapshot,
	); err != nil {
		return batchView{}, err
	}
	if len(snapshot.Usages) != wantUsages {
		return batchView{}, fmt.Errorf(
			"batch %s should have %d usage records, got %d",
			batchID,
			wantUsages,
			len(snapshot.Usages),
		)
	}
	return snapshot.Batch, nil
}

func fetchPlanUsage(client *http.Client, baseURL, planID, usageID string) (materialUsageView, error) {
	var snapshot planSnapshot
	if err := requestJSON(client, http.MethodGet, baseURL+"/plans/"+planID, nil, http.StatusOK, &snapshot); err != nil {
		return materialUsageView{}, err
	}
	for _, usage := range snapshot.Materials {
		if usage.ID == usageID {
			return usage, nil
		}
	}
	return materialUsageView{}, fmt.Errorf("plan %s is missing usage %s", planID, usageID)
}

func issueMaterial(
	client *http.Client,
	baseURL string,
	planID string,
	batchID string,
	quantity int,
	wantStatus int,
) (materialUsageView, error) {
	var usage materialUsageView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/materials",
		contracts.IssueMaterialRequest{BatchID: batchID, Quantity: quantity},
		wantStatus,
		&usage,
	)
	return usage, err
}

func consumeMaterial(
	client *http.Client,
	baseURL string,
	planID string,
	usageID string,
	quantity int,
	wantStatus int,
) (materialUsageView, error) {
	var usage materialUsageView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/materials/"+usageID+"/consume",
		contracts.MaterialQuantityRequest{Quantity: quantity},
		wantStatus,
		&usage,
	)
	return usage, err
}

func returnMaterial(
	client *http.Client,
	baseURL string,
	planID string,
	usageID string,
	quantity int,
	wantStatus int,
) (materialUsageView, error) {
	var usage materialUsageView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/materials/"+usageID+"/return",
		contracts.MaterialQuantityRequest{Quantity: quantity},
		wantStatus,
		&usage,
	)
	return usage, err
}

func concurrentConsume(client *http.Client, baseURL, planID, usageID string, quantity int) error {
	statuses := make(chan int, 2)
	url := baseURL + "/plans/" + planID + "/materials/" + usageID + "/consume"
	for i := 0; i < 2; i++ {
		go func() {
			status, err := postForStatus(client, url, contracts.MaterialQuantityRequest{Quantity: quantity})
			if err != nil {
				statuses <- -1
				return
			}
			statuses <- status
		}()
	}
	first, second := <-statuses, <-statuses
	matched := (first == http.StatusOK && second == http.StatusConflict) ||
		(first == http.StatusConflict && second == http.StatusOK)
	if !matched {
		return fmt.Errorf("concurrent consume should yield one 200 and one 409, got %d and %d", first, second)
	}
	return nil
}

func postForStatus(client *http.Client, url string, body any) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	response, err := client.Post(url, "application/json", bytes.NewReader(encoded))
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20)); err != nil {
		return 0, err
	}
	return response.StatusCode, nil
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
