package brainskills_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/brain_skill_selection"
	"automation-hub-backend/internal/brainskills"
	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
)

type mutationSelectionRepository struct {
	mu                 sync.Mutex
	events             []brain_skill_selection.SelectionEvent
	nextID             int64
	appendNewerDisable bool
}

func (r *mutationSelectionRepository) Append(ctx context.Context, event brain_skill_selection.SelectionEvent) (brain_skill_selection.SelectionEvent, error) {
	if err := ctx.Err(); err != nil {
		return brain_skill_selection.SelectionEvent{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	event.ID = r.nextID
	r.events = append(r.events, event)
	saved := event
	if r.appendNewerDisable && event.Enabled {
		r.nextID++
		newer := event
		newer.ID = r.nextID
		newer.Enabled = false
		newer.DecidedAt = event.DecidedAt.Add(time.Second)
		r.events = append(r.events, newer)
		r.appendNewerDisable = false
	}
	return saved, nil
}

func (r *mutationSelectionRepository) LatestForOwner(ctx context.Context, owner string) ([]brain_skill_selection.SelectionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	latest := make(map[string]brain_skill_selection.SelectionEvent)
	for _, event := range r.events {
		if event.OwnerIdentity != owner {
			continue
		}
		previous, exists := latest[event.SkillID]
		if !exists || event.ID > previous.ID {
			latest[event.SkillID] = event
		}
	}
	result := make([]brain_skill_selection.SelectionEvent, 0, len(latest))
	for _, event := range latest {
		result = append(result, event)
	}
	return result, nil
}

func TestServiceBackedSelectionMutationReturnsLatestSafeDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name              string
		concurrentDisable bool
		wantID            int64
		wantEnabled       bool
	}{
		{name: "new decision", wantID: 1, wantEnabled: true},
		{name: "later concurrent decision", concurrentDisable: true, wantID: 2, wantEnabled: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			const owner = "authenticated-owner@example.test"
			repository := &mutationSelectionRepository{appendNewerDisable: test.concurrentDisable}
			service := brain_skill_selection.NewSelectionService(repository, brainskills.DefaultCatalog())

			handler := brainskills.NewHandler(brain_skill_selection.NewOwnerSelectionStoreAdapter(service))
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(identity.ContextSubjectKey, owner)
				c.Next()
			})
			router.PUT("/:id/selection", handler.SetSelection)
			var skill brainskills.Skill
			for _, candidate := range brainskills.DefaultCatalog().List() {
				if candidate.ID == "frontend-design" {
					skill = candidate
					break
				}
			}
			if skill.ID == "" {
				t.Fatal("frontend-design is missing from the reviewed catalog")
			}
			requestBody := `{"enabled":true,"reviewedGuidanceSHA256":"` + skill.GuidanceSHA256 + `","reviewedCatalogFingerprint":"` + brainskills.DefaultCatalog().Fingerprint() + `"}`
			request := httptest.NewRequest(http.MethodPut, "/frontend-design/selection", strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode mutation response: %v", err)
			}
			if body["enabled"] == nil || body["needsReapproval"] == nil || body["selectionDecision"] == nil {
				t.Fatalf("mutation response fields = %v, want current state and decision", body)
			}
			if test.concurrentDisable {
				if len(body) != 4 || string(body["supersededByConcurrentDecision"]) != "true" {
					t.Fatalf("concurrent response = %v, want explicit superseded decision", body)
				}
			} else if len(body) != 3 || body["supersededByConcurrentDecision"] != nil {
				t.Fatalf("ordinary response fields = %v, want no concurrency notice", body)
			}
			var gotEnabled bool
			if err := json.Unmarshal(body["enabled"], &gotEnabled); err != nil {
				t.Fatalf("decode enabled state: %v", err)
			}
			if gotEnabled != test.wantEnabled {
				t.Fatalf("enabled = %t, want effective latest state %t", gotEnabled, test.wantEnabled)
			}
			var decision map[string]json.RawMessage
			if err := json.Unmarshal(body["selectionDecision"], &decision); err != nil {
				t.Fatalf("decode selection decision: %v", err)
			}
			if len(decision) != 3 || decision["id"] == nil || decision["actorIdentity"] == nil || decision["decidedAt"] == nil {
				t.Fatalf("decision fields = %v, want only id/actorIdentity/decidedAt", decision)
			}
			var gotID int64
			if err := json.Unmarshal(decision["id"], &gotID); err != nil {
				t.Fatalf("decode decision ID: %v", err)
			}
			if gotID != test.wantID {
				t.Fatalf("decision ID = %d, want latest event %d", gotID, test.wantID)
			}
			latest, err := repository.LatestForOwner(context.Background(), owner)
			if err != nil || len(latest) != 1 {
				t.Fatalf("load persisted latest decision: events=%d err=%v", len(latest), err)
			}
			if latest[0].ID != gotID || latest[0].Enabled != test.wantEnabled {
				t.Fatalf("response does not match persisted latest decision: response id=%d enabled=%t event=%#v", gotID, test.wantEnabled, latest[0])
			}
			var actor string
			if err := json.Unmarshal(decision["actorIdentity"], &actor); err != nil {
				t.Fatalf("decode actor identity: %v", err)
			}
			if actor != owner {
				t.Fatalf("actor identity = %q, want authenticated actor %q", actor, owner)
			}
			var decidedAt time.Time
			if err := json.Unmarshal(decision["decidedAt"], &decidedAt); err != nil {
				t.Fatalf("decode decision timestamp: %v", err)
			}
			if !decidedAt.Equal(latest[0].DecidedAt) {
				t.Fatalf("decision timestamp = %s, want persisted latest time %s", decidedAt, latest[0].DecidedAt)
			}
			for _, forbidden := range []string{"ownerIdentity", "ownerScope", "guidance", "summary"} {
				if strings.Contains(response.Body.String(), `"`+forbidden+`"`) {
					t.Errorf("mutation response exposed forbidden field %q", forbidden)
				}
			}
		})
	}
}
