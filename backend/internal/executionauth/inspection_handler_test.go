package executionauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/lifeontology"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestPublicReceiptExposesOnlyBoundedLifeGraphSummary(t *testing.T) {
	receipt := Receipt{
		ID: uuid.New(), Domain: string(lifeontology.DomainLegalGovernment),
		LifeGraphProjection: &lifeontology.OperationalProjectionResult{
			Primary:        lifeontology.Entity{ID: "life-entity-123", Domain: lifeontology.DomainLegalGovernment, OwnerIdentity: "private-owner"},
			LinkedEntities: []lifeontology.Entity{{ID: "linked-1", OwnerIdentity: "private-owner"}},
			Relations:      []lifeontology.Relation{{ID: "relation-1", OwnerIdentity: "private-owner"}},
			AlreadyExisted: true, AdvisoryOnly: true,
		},
	}
	view := publicReceipt(receipt)
	if view.LifeGraphProjection == nil || view.LifeGraphProjection.PrimaryID != "life-entity-123" ||
		view.LifeGraphProjection.Domain != string(lifeontology.DomainLegalGovernment) ||
		view.LifeGraphProjection.LinkedRecords != 1 || view.LifeGraphProjection.Relations != 1 ||
		!view.LifeGraphProjection.AdvisoryOnly || !view.LifeGraphProjection.AlreadyExisted {
		t.Fatalf("public life graph summary = %#v", view.LifeGraphProjection)
	}
}

func TestInspectionHandlerRequiresAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name  string
		value any
		set   bool
	}{
		{name: "missing"},
		{name: "blank", value: "  ", set: true},
		{name: "wrong type", value: []string{"alice"}, set: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := inspectionRouter(NewMemoryRepository(), func(c *gin.Context) {
				if test.set {
					c.Set(identity.ContextSubjectKey, test.value)
				}
			})
			response := performInspectionRequest(t, router, http.MethodGet, "/execution-authorizations")
			assertInspectionError(t, response, http.StatusUnauthorized, "authentication_required")
		})
	}
}

func TestInspectionHandlerScopesListGetAndConsumptionToOwner(t *testing.T) {
	repository := NewMemoryRepository()
	alice := inspectionFixtureReceipt("alice", "alice-action")
	bob := inspectionFixtureReceipt("bob", "bob-secret-action")
	mustStoreInspectionReceipt(t, repository, alice)
	mustStoreInspectionReceipt(t, repository, bob)
	if err := repository.Consume(context.Background(), Consumption{
		ReceiptID:       alice.ID,
		OwnerIdentity:   "alice",
		Consumer:        "safe-worker",
		ExecutionTarget: "workspace/result.txt",
		ReceiptDigest:   alice.DecisionDigest,
		ConsumedAt:      time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("consume alice receipt: %v", err)
	}

	router := inspectionRouter(repository, authenticatedInspectionOwner("alice"))
	list := performInspectionRequest(t, router, http.MethodGet, "/execution-authorizations")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var listBody struct {
		Receipts []inspectionReceipt `json:"receipts"`
		Count    int                 `json:"count"`
		Limit    int                 `json:"limit"`
	}
	decodeInspectionResponse(t, list, &listBody)
	if listBody.Count != 1 || len(listBody.Receipts) != 1 {
		t.Fatalf("alice list = %#v", listBody)
	}
	if listBody.Receipts[0].ID != alice.ID || strings.Contains(list.Body.String(), "bob-secret-action") {
		t.Fatalf("cross-owner list disclosure: %s", list.Body.String())
	}

	own := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/"+alice.ID.String())
	if own.Code != http.StatusOK {
		t.Fatalf("own get status = %d, body = %s", own.Code, own.Body.String())
	}

	foreign := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/"+bob.ID.String())
	assertInspectionError(t, foreign, http.StatusNotFound, "receipt_not_found")

	consumption := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/"+alice.ID.String()+"/consumption")
	if consumption.Code != http.StatusOK {
		t.Fatalf("consumption status = %d, body = %s", consumption.Code, consumption.Body.String())
	}
	var consumptionBody inspectionConsumption
	decodeInspectionResponse(t, consumption, &consumptionBody)
	if consumptionBody.ReceiptID != alice.ID || consumptionBody.Consumer != "safe-worker" {
		t.Fatalf("consumption body = %#v", consumptionBody)
	}

	foreignConsumption := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/"+bob.ID.String()+"/consumption")
	assertInspectionError(t, foreignConsumption, http.StatusNotFound, "consumption_not_found")
}

func TestInspectionHandlerReturnsCompactSummaryWithoutEvidence(t *testing.T) {
	repository := NewMemoryRepository()
	receipt := inspectionFixtureReceipt("alice", "automation.api.read")
	receipt.Outcome = OutcomeDenied
	receipt.Reason = "system workload effect does not match its registered operation contract"
	mustStoreInspectionReceipt(t, repository, receipt)

	router := inspectionRouter(repository, authenticatedInspectionOwner("alice"))
	response := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations?limit=25&view=summary")
	if response.Code != http.StatusOK {
		t.Fatalf("summary status = %d, body = %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decodeInspectionResponse(t, response, &body)
	receipts, ok := body["receipts"].([]any)
	if !ok || len(receipts) != 1 {
		t.Fatalf("summary receipts = %#v", body["receipts"])
	}
	view, ok := receipts[0].(map[string]any)
	if !ok {
		t.Fatalf("summary receipt = %#v", receipts[0])
	}
	if view["id"] != receipt.ID.String() || view["action"] != receipt.Action ||
		view["outcome"] != string(OutcomeDenied) || view["reason"] != receipt.Reason {
		t.Fatalf("summary identity = %#v", view)
	}
	for _, forbidden := range []string{
		"evidence", "requestFingerprint", "decisionFingerprint", "actorFingerprint",
		"requiredAuthority", "requestedAutonomy", "effectiveAutonomy",
	} {
		if _, exists := view[forbidden]; exists {
			t.Fatalf("summary exposed %s: %#v", forbidden, view)
		}
	}
}

func TestInspectionHandlerRejectsUnknownListView(t *testing.T) {
	router := inspectionRouter(NewMemoryRepository(), authenticatedInspectionOwner("alice"))
	response := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations?view=everything")
	assertInspectionError(t, response, http.StatusBadRequest, "invalid_view")
}

func TestInspectionHandlerDefendsAgainstFaultyReaderCrossOwnerResults(t *testing.T) {
	bob := inspectionFixtureReceipt("bob", "hidden")
	router := inspectionRouter(inspectionReaderStub{
		list: []Receipt{bob},
		get:  bob,
		consumption: Consumption{
			ReceiptID:     bob.ID,
			OwnerIdentity: "bob",
		},
	}, authenticatedInspectionOwner("alice"))

	list := performInspectionRequest(t, router, http.MethodGet, "/execution-authorizations")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), bob.ID.String()) {
		t.Fatalf("faulty list leaked cross-owner data: %d %s", list.Code, list.Body.String())
	}
	get := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/"+bob.ID.String())
	assertInspectionError(t, get, http.StatusNotFound, "receipt_not_found")
	consumption := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/"+bob.ID.String()+"/consumption")
	assertInspectionError(t, consumption, http.StatusNotFound, "consumption_not_found")
}

func TestInspectionHandlerBoundsAndRedactsPublicEvidence(t *testing.T) {
	repository := NewMemoryRepository()
	receipt := inspectionFixtureReceipt("alice@example.test", "automation.script.execute")
	receipt.ActorIdentity = "alice@example.test"
	receipt.IdempotencyKey = "never-expose-idempotency"
	receipt.RequestDigest = strings.Repeat("a", 64)
	receipt.DecisionDigest = strings.Repeat("b", 64)
	receipt.Reason = "authorization=super-secret " + strings.Repeat("x", 400)
	receipt.Evidence.Approval.ApprovedBy = "reviewer@example.test"
	receipt.Evidence.Approval.DecisionDigest = strings.Repeat("c", 64)
	receipt.Evidence.SystemWorkload = SystemWorkloadEvidence{
		PolicyID:      "fixture-workload-v1",
		ActorIdentity: "system:private-worker",
		Matched:       true,
	}
	receipt.Evidence.Trace = make([]string, 40)
	for index := range receipt.Evidence.Trace {
		receipt.Evidence.Trace[index] = "step token=hidden-" + strconv.Itoa(index)
	}
	mustStoreInspectionReceipt(t, repository, receipt)

	router := inspectionRouter(repository, authenticatedInspectionOwner("alice@example.test"))
	response := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/"+receipt.ID.String())
	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{
		"alice@example.test",
		"reviewer@example.test",
		"system:private-worker",
		"never-expose-idempotency",
		strings.Repeat("a", 64),
		strings.Repeat("b", 64),
		strings.Repeat("c", 64),
		"super-secret",
		"hidden-0",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response exposed %q: %s", forbidden, body)
		}
	}
	var view inspectionReceipt
	decodeInspectionResponse(t, response, &view)
	if len(view.Evidence.Trace) != maxInspectionListValues {
		t.Fatalf("trace entries = %d, want %d", len(view.Evidence.Trace), maxInspectionListValues)
	}
	if len([]rune(view.Reason)) > maxInspectionTextRunes {
		t.Fatalf("reason length = %d", len([]rune(view.Reason)))
	}
	if view.RequestFingerprint != strings.Repeat("a", inspectionFingerprintN) ||
		view.DecisionFingerprint != strings.Repeat("b", inspectionFingerprintN) {
		t.Fatalf("fingerprints = %q, %q", view.RequestFingerprint, view.DecisionFingerprint)
	}
	if view.Evidence.SystemWorkload.PolicyID != "fixture-workload-v1" ||
		!view.Evidence.SystemWorkload.Matched ||
		view.Evidence.SystemWorkload.ActorFingerprint == "" {
		t.Fatalf("system workload evidence = %#v", view.Evidence.SystemWorkload)
	}
}

func TestInspectionHandlerValidatesAndCapsListLimit(t *testing.T) {
	reader := &capturingInspectionReader{}
	router := inspectionRouter(reader, authenticatedInspectionOwner("alice"))

	for _, path := range []string{
		"/execution-authorizations?limit=0",
		"/execution-authorizations?limit=invalid",
	} {
		response := performInspectionRequest(t, router, http.MethodGet, path)
		assertInspectionError(t, response, http.StatusBadRequest, "invalid_limit")
	}
	response := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations?limit=9999")
	if response.Code != http.StatusOK {
		t.Fatalf("capped limit status = %d, body = %s", response.Code, response.Body.String())
	}
	if reader.limit != maxInspectionLimit || reader.owner != "alice" {
		t.Fatalf("reader called with owner=%q limit=%d", reader.owner, reader.limit)
	}
	if !strings.Contains(response.Body.String(), `"limit":100`) {
		t.Fatalf("response does not report effective limit: %s", response.Body.String())
	}

	invalidID := performInspectionRequest(t, router, http.MethodGet,
		"/execution-authorizations/not-a-uuid")
	assertInspectionError(t, invalidID, http.StatusBadRequest, "invalid_receipt_id")
}

func TestInspectionHandlerReturnsStableErrorsWithoutRepositoryDetail(t *testing.T) {
	reader := inspectionReaderStub{err: errors.New("postgres password=hunter2 dsn=secret")}
	router := inspectionRouter(reader, authenticatedInspectionOwner("alice"))

	for _, path := range []string{
		"/execution-authorizations",
		"/execution-authorizations/" + uuid.NewString(),
		"/execution-authorizations/" + uuid.NewString() + "/consumption",
	} {
		response := performInspectionRequest(t, router, http.MethodGet, path)
		assertInspectionError(t, response, http.StatusServiceUnavailable, "inspection_unavailable")
		if strings.Contains(response.Body.String(), "hunter2") ||
			strings.Contains(response.Body.String(), "postgres") {
			t.Fatalf("repository detail leaked: %s", response.Body.String())
		}
	}
}

func TestInspectionHandlerNilReaderFailsClosed(t *testing.T) {
	router := inspectionRouter(nil, authenticatedInspectionOwner("alice"))
	response := performInspectionRequest(t, router, http.MethodGet, "/execution-authorizations")
	assertInspectionError(t, response, http.StatusServiceUnavailable, "inspection_unavailable")
}

func inspectionFixtureReceipt(owner, action string) Receipt {
	id := uuid.New()
	return Receipt{
		ID:                   id,
		ContractVersion:      ContractVersion,
		OwnerIdentity:        owner,
		IdempotencyKey:       "intent-" + id.String(),
		ActorIdentity:        owner,
		ActorKind:            ActorHuman,
		TaskID:               "task-" + id.String(),
		Action:               action,
		Stage:                StageExecution,
		ResourceType:         "automation",
		ResourceID:           "resource-" + id.String(),
		EffectDigest:         strings.Repeat("e", 64),
		Outcome:              OutcomeAuthorized,
		Reason:               "authorization requirements satisfied",
		RequestDigest:        strings.Repeat("1", 64),
		DecisionDigest:       strings.Repeat("2", 64),
		RequiredAuthority:    4,
		RequestedAutonomy:    3,
		EffectiveAutonomy:    3,
		Risk:                 RiskLow,
		Reversible:           true,
		EstimatedCostEUR:     0,
		NotificationRequired: false,
		EvaluatedAt:          time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
		Evidence: DecisionEvidence{
			Constitution: ConstitutionEvidence{
				ID:                    "constitution-v1",
				Version:               1,
				Digest:                strings.Repeat("3", 64),
				RequestedCapabilities: []string{"filesystem-read"},
				AuthorityCeiling:      6,
			},
			ReasonCodes: []string{"authorized"},
			Trace:       []string{"constitution evaluated", "authorization granted"},
		},
	}
}

func mustStoreInspectionReceipt(t *testing.T, repository *MemoryRepository, receipt Receipt) {
	t.Helper()
	if _, _, err := repository.CreateOrGet(context.Background(), receipt); err != nil {
		t.Fatalf("store receipt: %v", err)
	}
}

func inspectionRouter(reader InspectionReader, middleware gin.HandlerFunc) http.Handler {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if middleware != nil {
		router.Use(middleware)
	}
	NewInspectionHandler(reader).RegisterRoutes(router.Group("/execution-authorizations"))
	return router
}

func authenticatedInspectionOwner(owner string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, owner)
		c.Next()
	}
}

func performInspectionRequest(
	t *testing.T,
	router http.Handler,
	method string,
	path string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertInspectionError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, status, response.Body.String())
	}
	var body inspectionErrorEnvelope
	decodeInspectionResponse(t, response, &body)
	if body.Error.Code != code || strings.TrimSpace(body.Error.Message) == "" {
		t.Fatalf("error = %#v, want code %q", body.Error, code)
	}
}

func decodeInspectionResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

type inspectionReaderStub struct {
	list        []Receipt
	get         Receipt
	consumption Consumption
	err         error
}

func (s inspectionReaderStub) List(context.Context, string, int) ([]Receipt, error) {
	return s.list, s.err
}

func (s inspectionReaderStub) Get(context.Context, string, uuid.UUID) (Receipt, error) {
	return s.get, s.err
}

func (s inspectionReaderStub) GetConsumption(
	context.Context,
	string,
	uuid.UUID,
) (Consumption, error) {
	return s.consumption, s.err
}

type capturingInspectionReader struct {
	owner string
	limit int
}

func (r *capturingInspectionReader) List(
	_ context.Context,
	owner string,
	limit int,
) ([]Receipt, error) {
	r.owner = owner
	r.limit = limit
	return nil, nil
}

func (r *capturingInspectionReader) Get(context.Context, string, uuid.UUID) (Receipt, error) {
	return Receipt{}, ErrNotFound
}

func (r *capturingInspectionReader) GetConsumption(
	context.Context,
	string,
	uuid.UUID,
) (Consumption, error) {
	return Consumption{}, ErrNotFound
}
