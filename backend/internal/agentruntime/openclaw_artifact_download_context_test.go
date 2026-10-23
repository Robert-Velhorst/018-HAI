package agentruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type artifactDownloadContextStore struct {
	fakeOpenClawGatewayReceiptStore
	reads  int
	onRead func()
}

func (s *artifactDownloadContextStore) FindOpenClawGatewayReceipt(reference string) (OpenClawGatewayReceipt, error) {
	s.reads++
	if s.onRead != nil {
		s.onRead()
	}
	return s.fakeOpenClawGatewayReceiptStore.FindOpenClawGatewayReceipt(reference)
}

func TestArtifactDownloadRejectsMissingOrCancelledContext(t *testing.T) {
	for _, stage := range []string{"missing", "before lookup", "during lookup"} {
		t.Run(stage, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected connection", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			receipt := OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), OwnerIdentity: "alice", RuntimeTaskID: "test-task", SessionKey: "agent:main:test", RunID: "test-run", TerminalStatus: "completed", TerminalAt: time.Now().UTC()}
			store := &artifactDownloadContextStore{fakeOpenClawGatewayReceiptStore: fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{receipt}}}
			adapter := &openClawAdapter{gatewayEnabled: true, gatewayArtifactImportEnabled: true, gatewayToken: "fixture-token", gatewayURL: "ws" + strings.TrimPrefix(server.URL, "http"), timeout: time.Second, allowedHost: map[string]bool{"127.0.0.1": true}, gatewayReceiptStore: store}
			registry := &Registry{adapters: map[string]Adapter{"openclaw": adapter}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var input context.Context = ctx
			wantReads := 0
			switch stage {
			case "missing":
				input = nil
			case "before lookup":
				cancel()
			case "during lookup":
				store.onRead = cancel
				wantReads = 1
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("download panicked on invalid context: %v", recovered)
				}
			}()
			content, err := registry.DownloadOpenClawGatewayArtifact(input, receipt, GatewayArtifactDescriptor{Digest: strings.Repeat("a", 64), Type: "file"})
			if content != nil || err == nil {
				t.Fatal("invalid context accepted")
			}
			if stage != "missing" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation identity lost: %v", err)
			}
			if store.reads != wantReads || requests.Load() != 0 {
				t.Fatalf("unexpected effects: reads=%d network=%d", store.reads, requests.Load())
			}
		})
	}
}
