package ambient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	pursuitpkg "automation-hub-backend/internal/pursuit"

	"github.com/gin-gonic/gin"
)

type ambientPersonalAckRepository struct {
	*ambientOwnerRetentionProbe
	missingCreation bool
	missingOutcome  bool
	foreignCreation bool
	foreignOutcome  bool
	outcomeError    error
	updates         int
}

func (r *ambientPersonalAckRepository) CreateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	created, err := r.ambientRepositoryStub.CreateScan(scan)
	if r.missingCreation {
		return nil, err
	}
	if r.foreignCreation {
		created.OwnerIdentity = "foreign-owner"
	}
	return created, err
}

func (r *ambientPersonalAckRepository) UpdateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	r.updates++
	if r.missingOutcome || r.outcomeError != nil {
		return nil, r.outcomeError
	}
	if r.foreignOutcome {
		scan.OwnerIdentity = "foreign-owner"
	}
	return scan, nil
}

type ambientPersonalAckPursuits struct {
	*ambientPursuitSpy
	cause error
	calls int
}

func (p *ambientPersonalAckPursuits) DashboardForOwner(owner string) (*pursuitpkg.Dashboard, error) {
	p.calls++
	if p.cause != nil {
		return nil, p.cause
	}
	return p.ambientPursuitSpy.DashboardForOwner(owner)
}

// The HTTP handler and actual service run together with a controlled verified
// principal and storage. This does not exercise a live IDP or database server.
func TestAmbientPersonalScanHTTPRequiresAcknowledgedOwnedOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"completed", "missing-creation", "foreign-creation", "missing-outcome", "foreign-outcome", "storage-error", "failed-unconfirmed", "failed-confirmed"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &ambientPersonalAckRepository{ambientOwnerRetentionProbe: &ambientOwnerRetentionProbe{ambientRepositoryStub: &ambientRepositoryStub{}}}
			pursuits := &ambientPersonalAckPursuits{ambientPursuitSpy: &ambientPursuitSpy{}}
			switch scenario {
			case "missing-creation":
				repo.missingCreation = true
			case "foreign-creation":
				repo.foreignCreation = true
			case "missing-outcome":
				repo.missingOutcome = true
			case "foreign-outcome":
				repo.foreignOutcome = true
			case "storage-error", "failed-unconfirmed":
				repo.outcomeError = errors.New("storage token=must-not-leak")
			}
			if strings.HasPrefix(scenario, "failed-") {
				pursuits.cause = errors.New("source token=must-not-leak")
			}
			handler := NewHandler(NewServiceWithPursuits(repo, nil, nil, pursuits))
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Set(identity.ContextSubjectKey, "alice")
				c.Next()
			})
			engine.POST("/ambient/scan", handler.Scan)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/ambient/scan", nil))
			var body struct {
				Error string              `json:"error"`
				Scan  *models.AmbientScan `json:"scan"`
			}
			if strings.Contains(recorder.Body.String(), "must-not-leak") || strings.Contains(recorder.Body.String(), "foreign-owner") {
				t.Fatal("public outcome leaked credentials or foreign scope")
			}
			if scenario == "completed" {
				if recorder.Code != http.StatusOK || repo.updates != 1 || repo.global != 0 || len(repo.owners) != 1 || repo.owners[0] != "alice" {
					t.Fatalf("known completion failed: status=%d updates=%d", recorder.Code, repo.updates)
				}
				return
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			want := "outcome_unconfirmed"
			if scenario == "failed-confirmed" {
				want = "failed"
			}
			if recorder.Code != http.StatusInternalServerError || body.Scan == nil || body.Scan.Status != want || body.Scan.OwnerIdentity != "alice" || repo.global != 0 || len(repo.owners) != 0 {
				t.Fatalf("HTTP acknowledged invalid outcome: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if strings.HasSuffix(scenario, "creation") && (pursuits.calls != 0 || repo.updates != 0) {
				t.Fatal("unconfirmed creation entered personal context or outcome write")
			}
			if scenario == "storage-error" && repo.updates != 1 {
				t.Fatal("ambiguous terminal write was retried as a failure write")
			}
		})
	}
}
