package automation

import (
	"automation-hub-backend/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type launchInputProbe struct {
	Service
	calls   int
	request TaskLaunchRequest
}

func (s *launchInputProbe) LaunchTask(id uuid.UUID, request TaskLaunchRequest) (*LaunchResult, error) {
	s.calls++
	s.request = request
	return &LaunchResult{AutomationID: id, Status: "ready"}, nil
}
func TestLaunchRequestConsumesOneBoundedJSONValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	large := `{"task":"` + strings.Repeat("x", 65536) + `"}`
	for _, item := range []struct {
		name, body    string
		unknownLength bool
		want          int
	}{
		{"two_objects", `{} {"task":"different"}`, false, http.StatusBadRequest},
		{"trailing_literal", `{} true`, false, http.StatusBadRequest},
		{"chunked_malformed", `{"task":`, true, http.StatusBadRequest},
		{"null", `null`, false, http.StatusBadRequest},
		{"array", `[]`, false, http.StatusBadRequest},
		{"oversized", large, false, http.StatusRequestEntityTooLarge},
		{"chunked_oversized", large, true, http.StatusRequestEntityTooLarge},
		{"oversized_trailing_whitespace", `{}` + strings.Repeat(" ", 65536), true, http.StatusRequestEntityTooLarge},
	} {
		t.Run(item.name, func(t *testing.T) {
			probe := &launchInputProbe{}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Set(identity.ContextSubjectKey, "alice")
			c.Request = httptest.NewRequest(http.MethodPost, "/launch", strings.NewReader(item.body))
			if item.unknownLength {
				c.Request.ContentLength = -1
			}
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			NewHandler(probe).Launch(c)
			if response.Code != item.want || probe.calls != 0 {
				t.Fatalf("invalid body reached launch: status=%d calls=%d", response.Code, probe.calls)
			}
		})
	}
}

func TestLaunchRequestReadsUnknownLengthContext(t *testing.T) {
	probe := &launchInputProbe{}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set(identity.ContextSubjectKey, "alice")
	c.Request = httptest.NewRequest(http.MethodPost, "/launch", strings.NewReader(`{"task":"requested task","projectKey":"018-hai","idempotencyKey":"same-attempt"}`))
	c.Request.ContentLength = -1
	c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
	NewHandler(probe).Launch(c)
	if response.Code != http.StatusOK || probe.calls != 1 || probe.request.Task != "requested task" || probe.request.ProjectKey != "018-hai" || probe.request.IdempotencyKey != "same-attempt" {
		t.Fatal("unknown-length launch silently discarded supplied context")
	}
}

func TestLaunchRequestOptionalBodyAndExactLimit(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"task":"` + strings.Repeat("x", 65536-11) + `"}`} {
		probe := &launchInputProbe{}
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Set(identity.ContextSubjectKey, "alice")
		c.Request = httptest.NewRequest(http.MethodPost, "/launch", strings.NewReader(body))
		// A zero declared length must not cause an actual body to be ignored.
		c.Request.ContentLength = 0
		c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
		NewHandler(probe).Launch(c)
		if response.Code != http.StatusOK || probe.calls != 1 {
			t.Fatal("optional body or exact-size JSON was refused")
		}
		if len(body) == 65536 && len(probe.request.Task) != 65536-11 {
			t.Fatal("exact-limit body was silently discarded")
		}
	}
}
