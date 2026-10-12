package openclawmaintenance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func verifiedCompanionCapability() WorkerCapabilityHandshake {
	return WorkerCapabilityHandshake{
		ProtocolVersion:         workerCapabilityProtocolVersion,
		WorkerID:                "windows-openclaw-maintenance",
		Platform:                "windows",
		Target:                  "companion",
		Supported:               true,
		Containment:             companionJobContainmentID,
		CreateSuspended:         true,
		AssignBeforeResume:      true,
		KillOnClose:             true,
		WaitForJobEmpty:         true,
		UnknownOnUnverifiedExit: true,
	}
}

func TestCapabilityHandshakeRequiresEveryWindowsContainmentGuarantee(t *testing.T) {
	valid := verifiedCompanionCapability()
	if !valid.supportsCompanionInstall() {
		t.Fatal("complete Windows Job Object capability was rejected")
	}
	tests := []struct {
		name   string
		mutate func(*WorkerCapabilityHandshake)
	}{
		{"wrong protocol", func(c *WorkerCapabilityHandshake) { c.ProtocolVersion++ }},
		{"wrong platform", func(c *WorkerCapabilityHandshake) { c.Platform = "linux" }},
		{"wrong target", func(c *WorkerCapabilityHandshake) { c.Target = "gateway_core" }},
		{"no support", func(c *WorkerCapabilityHandshake) { c.Supported = false }},
		{"no atomic suspended creation", func(c *WorkerCapabilityHandshake) { c.CreateSuspended = false }},
		{"assignment after resume", func(c *WorkerCapabilityHandshake) { c.AssignBeforeResume = false }},
		{"no kill on close", func(c *WorkerCapabilityHandshake) { c.KillOnClose = false }},
		{"no job-empty wait", func(c *WorkerCapabilityHandshake) { c.WaitForJobEmpty = false }},
		{"unknown termination omitted", func(c *WorkerCapabilityHandshake) { c.UnknownOnUnverifiedExit = false }},
		{"wrong containment", func(c *WorkerCapabilityHandshake) { c.Containment = "windows-process-only" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if candidate.supportsCompanionInstall() {
				t.Fatal("incomplete capability was accepted")
			}
		})
	}
}

func TestCapabilityHeaderIsStrictAndBounded(t *testing.T) {
	data, err := json.Marshal(verifiedCompanionCapability())
	if err != nil {
		t.Fatal(err)
	}
	if got, supported := capabilityFromHeader(string(data)); !supported || !got.supportsCompanionInstall() {
		t.Fatal("valid capability header was rejected")
	}
	for _, value := range []string{
		"",
		"{",
		string(data) + string(data),
		strings.TrimSuffix(string(data), "}") + `,"unexpected":true}`,
		strings.Repeat("x", 1025),
	} {
		if _, supported := capabilityFromHeader(value); supported {
			t.Fatalf("accepted invalid capability header of length %d", len(value))
		}
	}
}

func TestPullClientSendsCapabilityOnAuthenticatedPoll(t *testing.T) {
	token, backendKey := strings.Repeat("t", 64), strings.Repeat("k", 64)
	var received WorkerCapabilityHandshake
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-HAI-Backend-Key") != backendKey {
			t.Errorf("poll credentials were not attached")
		}
		var ok bool
		received, ok = capabilityFromHeader(r.Header.Get("X-HAI-Worker-Capability"))
		if !ok {
			t.Errorf("authenticated poll omitted a valid capability handshake")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewPullClient(server.URL, token, backendKey)
	if err != nil {
		t.Fatal(err)
	}
	err = client.PollOnceWithCapabilities(context.Background(), verifiedCompanionCapability(), func(context.Context, Job, func() error) (Report, error) {
		t.Fatal("executor ran despite an empty lease response")
		return Report{}, nil
	})
	if err != nil || !received.supportsCompanionInstall() {
		t.Fatalf("capability handshake was not transported: capability=%+v err=%v", received, err)
	}
}

func TestInstallationCapabilityExpiresAndKeepsGatewayBlocked(t *testing.T) {
	h := &Handler{enabled: true}
	if got := h.targetInstallationCapability("companion", time.Now()); got.Available || got.State != "unknown" {
		t.Fatalf("missing worker proof = %+v", got)
	}
	proof := verifiedCompanionCapability()
	h.recordWorkerContact(proof, true)
	now := time.Now().UTC()
	if got := h.targetInstallationCapability("companion", now); !got.Available || got.State != "available" {
		t.Fatalf("fresh worker proof = %+v", got)
	}
	if got := h.targetInstallationCapability("companion", now.Add(maintenanceWorkerCapabilityTTL+time.Nanosecond)); got.Available || got.State != "stale" {
		t.Fatalf("expired worker proof = %+v", got)
	}
	if got := h.targetInstallationCapability("gateway_core", now); got.Available || got.State != "blocked" || !strings.Contains(got.Reason, "WSL") {
		t.Fatalf("Gateway/core inherited Windows capability: %+v", got)
	}
	h.recordWorkerContact(WorkerCapabilityHandshake{}, false)
	if got := h.targetInstallationCapability("companion", time.Now().UTC()); got.Available || got.State != "unsupported" {
		t.Fatalf("unsupported fresh worker report = %+v", got)
	}
}

func TestConcurrentCapabilityRefreshAndReadIsRaceSafe(t *testing.T) {
	h := &Handler{enabled: true}
	proof := verifiedCompanionCapability()
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			if i%2 == 0 {
				h.recordWorkerContact(proof, true)
				return
			}
			for j := 0; j < 100; j++ {
				_ = h.targetInstallationCapability("companion", time.Now().UTC())
				_ = h.workerContactStatus(time.Now().UTC())
			}
		}(i)
	}
	group.Wait()
}
