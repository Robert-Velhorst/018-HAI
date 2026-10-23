package openclawmaintenance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWorkerAuthRejectsAmbiguousCredentialsAndContainmentHeaders(t *testing.T) {
	token := strings.Repeat("w", 64)
	capability, err := json.Marshal(verifiedCompanionCapability())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name         string
		auth         []string
		capabilities []string
		wantStatus   int
		wantContact  bool
		wantInstall  bool
	}{
		{"single verified header", []string{"Bearer " + token}, []string{string(capability)}, 204, true, true},
		{"duplicate valid credentials", []string{"Bearer " + token, "Bearer " + token}, []string{string(capability)}, 401, false, false},
		{"valid then wrong credential", []string{"Bearer " + token, "Bearer wrong"}, []string{string(capability)}, 401, false, false},
		{"wrong then valid credential", []string{"Bearer wrong", "Bearer " + token}, []string{string(capability)}, 401, false, false},
		{"duplicate valid capabilities", []string{"Bearer " + token}, []string{string(capability), string(capability)}, 204, true, false},
		{"conflicting capabilities", []string{"Bearer " + token}, []string{string(capability), `{}`}, 204, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := &Handler{enabled: true, token: token}
			router := gin.New()
			router.POST("/", h.WorkerAuth, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			for _, value := range test.auth {
				req.Header.Add("Authorization", value)
			}
			for _, value := range test.capabilities {
				req.Header.Add("X-HAI-Worker-Capability", value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d", response.Code, test.wantStatus)
			}
			if got := !h.workerLastSeen.IsZero(); got != test.wantContact {
				t.Fatalf("authenticated contact=%t want=%t", got, test.wantContact)
			}
			if got := h.targetInstallationCapability("companion", time.Now().UTC()).Available; got != test.wantInstall {
				t.Fatalf("installation capability=%t want=%t", got, test.wantInstall)
			}
		})
	}
}

func TestWorkerAmbiguousHeadersCannotLeasePendingInstallPostgres(t *testing.T) {
	for _, duplicateCredential := range []bool{false, true} {
		name := "duplicate capability"
		if duplicateCredential {
			name = "duplicate credential"
		}
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			completeCheckFor(t, s, "companion")
			if err := s.Policy(ctx, "companion", "auto_install_verified", "test-owner"); err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(ctx, "companion", "2026.9.1"); err != nil {
				t.Fatal(err)
			}
			loseReceipt := false
			client := testPullAPI(t, s, &loseReceipt)
			transport := client.http.Transport
			client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				request = request.Clone(request.Context())
				if duplicateCredential {
					request.Header.Add("Authorization", request.Header.Get("Authorization"))
				} else {
					request.Header.Add("X-HAI-Worker-Capability", `{}`)
				}
				return transport.RoundTrip(request)
			})
			executions := 0
			err := client.PollOnceWithCapabilities(ctx, verifiedCompanionCapability(), func(context.Context, Job, func() error) (Report, error) {
				executions++
				return Report{}, nil
			})
			if (err != nil) != duplicateCredential || executions != 0 {
				t.Fatalf("ambiguous headers admitted execution: executions=%d err=%v", executions, err)
			}
			var job Job
			if err := s.db.Where("target = ? AND kind = ?", "companion", "apply").First(&job).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus := "cancelled"
			if duplicateCredential {
				wantStatus = "pending"
			}
			if job.Status != wantStatus || job.Worker != "" || job.LeaseDigest != "" || job.StartedAt != nil {
				t.Fatalf("unexpected durable admission state: status=%s worker=%q started=%v", job.Status, job.Worker, job.StartedAt)
			}
			if !duplicateCredential {
				var result map[string]string
				if json.Unmarshal([]byte(job.Result), &result) != nil || result["reason"] != "companion_worker_capability_unavailable" || job.FinishedAt == nil {
					t.Fatal("capability rejection did not retain its durable cancellation reason")
				}
			}
		})
	}
}
