package automation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type runtimeIdentityProbe struct {
	Service
	calls       int
	owner       string
	stopContext context.Context
	legacyStops int
}

type countedRuntimeBody struct{ reads int }

func (b *countedRuntimeBody) Read(_ []byte) (int, error) {
	b.reads++
	return 0, io.EOF
}

func (p *runtimeIdentityProbe) LaunchTask(id uuid.UUID, request TaskLaunchRequest) (*LaunchResult, error) {
	p.calls++
	p.owner = request.OwnerIdentity
	return &LaunchResult{AutomationID: id, Status: "ready"}, nil
}
func (p *runtimeIdentityProbe) StopRuntimeTaskForOwner(id uuid.UUID, owner string) (*agentruntime.StopResult, error) {
	p.legacyStops++
	p.calls++
	p.owner = owner
	return &agentruntime.StopResult{Status: "completed"}, nil
}
func (p *runtimeIdentityProbe) StopRuntimeTaskForOwnerContext(ctx context.Context, _ uuid.UUID, owner string) (*agentruntime.StopResult, error) {
	p.calls++
	p.owner, p.stopContext = owner, ctx
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &agentruntime.StopResult{Status: "cancellation_requested"}, nil
}

func TestRuntimeStopHandlerPassesOriginalRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			probe := &runtimeIdentityProbe{}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/runtime", nil).WithContext(ctx)
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			c.Set(identity.ContextSubjectKey, "alice")
			NewHandler(probe).StopRuntimeTask(c)
			if probe.stopContext != ctx || probe.legacyStops != 0 || probe.calls != 1 {
				t.Fatal("handler detached request context or used legacy stop")
			}
			want := http.StatusOK
			if cancelled {
				want = http.StatusInternalServerError
			}
			if response.Code != want {
				t.Fatal("stop context failure was hidden")
			}
		})
	}
}
func (p *runtimeIdentityProbe) DiagnosticsForOwner(id uuid.UUID, owner string) (*DiagnosticResult, error) {
	p.calls++
	p.owner = owner
	return &DiagnosticResult{AutomationID: id}, nil
}
func TestRuntimeHandlersRequireVerifiedActorWithoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"launch", "stop", "diagnostics"} {
		for _, actor := range []any{nil, "", "  ", 42, " alice "} {
			t.Run(operation+"/"+actorCaseName(actor), func(t *testing.T) {
				probe := &runtimeIdentityProbe{}
				handler := NewHandler(probe)
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodPost, "/runtime", nil)
				body := &countedRuntimeBody{}
				c.Request.Body = io.NopCloser(body)
				c.Request.ContentLength = -1
				c.Request.Header.Set("Authorization", "Bearer client-claim-not-verified")
				c.Request.Header.Set("X-User-Identity", "forged-owner")
				c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
				if actor != nil {
					c.Set(identity.ContextSubjectKey, actor)
				}
				switch operation {
				case "launch":
					handler.Launch(c)
				case "stop":
					handler.StopRuntimeTask(c)
				case "diagnostics":
					handler.Diagnostics(c)
				}
				if actor == " alice " {
					if response.Code != http.StatusOK || probe.calls != 1 || probe.owner != "alice" {
						t.Fatal("verified actor did not reach owner-scoped service")
					}
				} else if response.Code != http.StatusUnauthorized || probe.calls != 0 || body.reads != 0 {
					t.Fatalf("invalid actor reached service/body: status=%d calls=%d reads=%d", response.Code, probe.calls, body.reads)
				}
			})
		}
	}
}
func actorCaseName(actor any) string {
	if actor == nil {
		return "missing"
	}
	if actor == "" {
		return "empty"
	}
	if actor == "  " {
		return "whitespace"
	}
	if actor == " alice " {
		return "verified"
	}
	return "wrong_type"
}
