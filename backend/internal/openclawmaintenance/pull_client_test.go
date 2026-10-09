package openclawmaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPullClientConfiguration(t *testing.T) {
	for _, address := range []string{"", "https://127.0.0.1", "http://localhost", "http://example.com", "http://user@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?secret=x", "http://127.0.0.1#x"} {
		if _, err := NewPullClient(address, strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
			t.Errorf("accepted %q", address)
		}
	}
	for _, keys := range [][2]string{{"short", strings.Repeat("b", 64)}, {strings.Repeat("a", 64), ""}, {strings.Repeat("a", 64), strings.Repeat("a", 64)}, {strings.Repeat("a", 64) + "\n", strings.Repeat("b", 64)}} {
		if _, err := NewPullClient("http://127.0.0.1", keys[0], keys[1]); err == nil {
			t.Error("accepted invalid credentials")
		}
	}
}

func TestPullClientCycle(t *testing.T) {
	for _, scenario := range []string{"ok", "idle", "confirm_denied", "permit_denied", "permit_revoked_during_apply", "permit_revoked_receipt_denied", "permit_revoked_receipt_unconfirmed", "backend_health_rejected", "invalid_ack", "receipt_retry", "receipt_lost", "execution_failed", "reported_failure", "malformed_report", "invalid_lease", "oversized", "unauthorized"} {
		t.Run(scenario, func(t *testing.T) {
			token, key := strings.Repeat("a", 64), strings.Repeat("b", 64)
			until := time.Now().Add(20 * time.Minute)
			lease := Lease{Job: Job{ID: uuid.NewString(), Target: "gateway_core", Kind: "apply", Status: "leased", Version: "2026.9.1", Evidence: strings.Repeat("c", 64), LeaseUntil: &until}, Token: strings.Repeat("d", 64)}
			executed, installed, completions := 0, 0, 0
			var permitCalls atomic.Int32
			var receipts []Report
			runStarted := make(chan struct{})
			releaseExecution := make(chan struct{})
			defer close(releaseExecution)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-HAI-Backend-Key") != key {
					t.Error("missing worker authentication")
					w.WriteHeader(401)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/leases") {
					switch scenario {
					case "idle":
						w.WriteHeader(204)
						return
					case "unauthorized":
						w.WriteHeader(401)
						return
					case "oversized":
						w.Write([]byte(strings.Repeat("x", 16385)))
						return
					case "invalid_lease":
						lease.Job.ID = "../another-route"
					}
					json.NewEncoder(w).Encode(lease)
					return
				}
				var body struct {
					Token  string `json:"leaseToken"`
					Report Report `json:"report"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Token != lease.Token {
					t.Error("lease token missing")
					w.WriteHeader(400)
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/confirm"):
					if scenario == "confirm_denied" {
						w.WriteHeader(409)
						return
					}
				case strings.HasSuffix(r.URL.Path, "/permit"):
					calls := permitCalls.Add(1)
					revokedDuringApply := scenario == "permit_revoked_during_apply" || scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed"
					if revokedDuringApply && calls == 1 {
						<-runStarted
					}
					if scenario == "permit_denied" || (revokedDuringApply && calls > 1) {
						w.WriteHeader(409)
						return
					}
				case strings.HasSuffix(r.URL.Path, "/complete"):
					completions++
					receipts = append(receipts, body.Report)
					if scenario == "permit_revoked_receipt_denied" {
						w.WriteHeader(409)
						return
					}
					if scenario == "permit_revoked_receipt_unconfirmed" {
						w.WriteHeader(503)
						return
					}
					if scenario == "receipt_lost" || (scenario == "receipt_retry" && completions < 3) {
						w.WriteHeader(503)
						return
					}
					status := "completed"
					if scenario == "backend_health_rejected" {
						status = "needs_review"
					}
					w.Header().Set("Content-Type", "application/json")
					if scenario == "invalid_ack" {
						_, _ = w.Write([]byte(`{"status":"unknown"}`))
						return
					}
					_ = json.NewEncoder(w).Encode(ReceiptAcknowledgement{Status: status})
					return
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			client, err := NewPullClient(server.URL, token, key)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "permit_revoked_during_apply" || scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed" {
				client.permitInterval = time.Millisecond
				client.cancelWait = 20 * time.Millisecond
			}
			providerMetadataAt := freshTestProviderMetadata()
			err = client.PollOnce(context.Background(), func(ctx context.Context, job Job, permit func() error) (Report, error) {
				executed++
				if _, ok := ctx.Deadline(); !ok {
					t.Error("execution deadline missing")
				}
				revokedDuringApply := scenario == "permit_revoked_during_apply" || scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed"
				if !revokedDuringApply {
					if err := permit(); err != nil {
						return Report{}, err
					}
				}
				if revokedDuringApply {
					close(runStarted)
					if scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed" {
						<-releaseExecution
						return Report{}, ctx.Err()
					}
					<-ctx.Done()
					return Report{}, ctx.Err()
				}
				if scenario == "execution_failed" {
					return Report{}, errors.New("secret raw provider error")
				}
				if scenario == "reported_failure" {
					return Report{Installed: job.Version, Evidence: job.Evidence, Outcome: "update_failed"}, nil
				}
				if scenario == "malformed_report" {
					return Report{Installed: "secret raw provider error", Outcome: "ok"}, nil
				}
				installed++
				return Report{Installed: job.Version, Available: job.Version, Evidence: job.Evidence, ProviderMetadataAt: providerMetadataAt, PublisherVerified: true, HealthOK: true, Outcome: "ok"}, nil
			})
			wantError := scenario != "ok" && scenario != "idle" && scenario != "receipt_retry"
			if (err != nil) != wantError {
				t.Fatalf("error %v scenario %s", err, scenario)
			}
			if err != nil && strings.Contains(err.Error(), "secret raw") {
				t.Fatal("provider error leaked")
			}
			if scenario == "permit_revoked_during_apply" || scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed" {
				if !errors.Is(err, ErrWorkerStopAfterCancellation) {
					t.Fatalf("interrupted installation must stop the worker poll loop: %v", err)
				}
			}
			if (scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed") && !errors.Is(err, ErrExecutionCancellationUnconfirmed) {
				t.Fatalf("unconfirmed cancellation must remain visible: %v", err)
			}
			blocked := scenario == "idle" || scenario == "confirm_denied" || scenario == "invalid_lease" || scenario == "oversized" || scenario == "unauthorized"
			if blocked && executed != 0 {
				t.Fatal("executed without a confirmed lease")
			}
			if !blocked && executed != 1 {
				t.Fatalf("executed %d times", executed)
			}
			if scenario == "backend_health_rejected" && (installed != 1 || completions != 1 || !strings.Contains(err.Error(), "recorded as needs_review")) {
				t.Fatalf("backend health rejection was not surfaced without replay: installed=%d completions=%d err=%v", installed, completions, err)
			}
			if scenario == "invalid_ack" && (installed != 1 || completions != 3) {
				t.Fatalf("invalid acknowledgement did not trigger bounded exact retries: installed=%d completions=%d", installed, completions)
			}
			if (scenario == "permit_denied" || scenario == "permit_revoked_during_apply" || scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed" || scenario == "execution_failed") && (installed != 0 || receipts[0].Outcome != "needs_review") {
				t.Fatal("failed action did not persist review outcome")
			}
			if (scenario == "permit_revoked_during_apply" || scenario == "permit_revoked_receipt_denied" || scenario == "permit_revoked_receipt_unconfirmed") && permitCalls.Load() != 2 {
				t.Fatalf("expected two monitored permit checks, got %d", permitCalls.Load())
			}
			if scenario == "permit_revoked_receipt_denied" && completions != 1 {
				t.Fatalf("do not retry a rejected review receipt, got %d attempts", completions)
			}
			if scenario == "permit_revoked_receipt_unconfirmed" && completions != 3 {
				t.Fatalf("expected bounded receipt retries, got %d", completions)
			}
			if scenario == "receipt_retry" || scenario == "receipt_lost" {
				if installed != 1 || completions != 3 || !reflect.DeepEqual(receipts[0], receipts[1]) || !reflect.DeepEqual(receipts[1], receipts[2]) {
					t.Fatal("receipt retry changed or repeated execution")
				}
			}
		})
	}
}

func TestPullClientNeverFollowsRedirect(t *testing.T) {
	requests := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(204) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	client, err := NewPullClient(redirect.URL, strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	err = client.PollOnce(context.Background(), func(context.Context, Job, func() error) (Report, error) {
		t.Fatal("unexpected execution")
		return Report{}, nil
	})
	if err == nil || requests != 0 {
		t.Fatal("redirect followed or accepted")
	}
}

func TestPullClientRecordsReviewWhenParentStopsDuringApply(t *testing.T) {
	token, key := strings.Repeat("a", 64), strings.Repeat("b", 64)
	until := time.Now().Add(20 * time.Minute)
	lease := Lease{Job: Job{ID: uuid.NewString(), Target: "gateway_core", Kind: "apply", Status: "leased", Version: "2026.9.1", Evidence: strings.Repeat("c", 64), LeaseUntil: &until}, Token: strings.Repeat("d", 64)}
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-HAI-Backend-Key") != key {
			t.Error("missing worker authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/leases"):
			_ = json.NewEncoder(w).Encode(lease)
		case strings.HasSuffix(r.URL.Path, "/confirm"), strings.HasSuffix(r.URL.Path, "/permit"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/complete"):
			var body struct {
				Token  string `json:"leaseToken"`
				Report Report `json:"report"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Token != lease.Token || body.Report.Outcome != "needs_review" {
				t.Error("cancelled update was not recorded for owner review")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ReceiptAcknowledgement{Status: "needs_review"})
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewPullClient(server.URL, token, key)
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- client.PollOnce(parent, func(ctx context.Context, _ Job, permit func() error) (Report, error) {
			if err := permit(); err != nil {
				return Report{}, err
			}
			close(started)
			<-ctx.Done()
			return Report{}, ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("update executor did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if err == nil || !errors.Is(err, ErrWorkerStopAfterCancellation) || !strings.Contains(err.Error(), "recorded the update as needs_review") || !strings.Contains(err.Error(), "inspect the recorded outcome") {
			t.Fatalf("expected recorded review outcome, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled update did not return after recording its receipt")
	}
}

func TestPermitRevocationBoundsNonContextAwareExecutorWait(t *testing.T) {
	job := Job{Target: "gateway_core", Kind: "apply", Evidence: strings.Repeat("c", 64)}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	permitErr := errors.New("permission revoked")
	finished := make(chan struct {
		report Report
		err    error
	}, 1)
	go func() {
		report, err := executeWithPermitMonitor(context.Background(), job, time.Millisecond, 10*time.Millisecond, func(context.Context) error {
			<-started
			return permitErr
		}, func(context.Context) (Report, error) {
			close(started)
			<-release
			return Report{Evidence: job.Evidence, ProviderMetadataAt: freshTestProviderMetadata(), Outcome: "ok"}, nil
		})
		finished <- struct {
			report Report
			err    error
		}{report: report, err: err}
	}()
	select {
	case result := <-finished:
		if !errors.Is(result.err, ErrWorkerStopAfterCancellation) || !errors.Is(result.err, ErrExecutionCancellationUnconfirmed) || !errors.Is(result.err, permitErr) || result.report.Outcome != "needs_review" {
			t.Fatalf("non-context-aware work was not bounded for review: report=%+v err=%v", result.report, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel wait exceeded its configured bound")
	}
}

func TestExecutorReturningContextCancellationStopsWorker(t *testing.T) {
	job := Job{Target: "gateway_core", Kind: "apply", Evidence: strings.Repeat("c", 64)}
	_, err := executeWithPermitMonitor(context.Background(), job, time.Millisecond, 10*time.Millisecond,
		func(context.Context) error { return nil },
		func(context.Context) (Report, error) { return Report{}, context.Canceled },
	)
	if !errors.Is(err, ErrWorkerStopAfterCancellation) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled executor result did not stop worker: %v", err)
	}
}

func TestPulledLeaseValidation(t *testing.T) {
	until := time.Now().Add(time.Minute)
	valid := Lease{Job: Job{ID: uuid.NewString(), Target: "gateway_core", Kind: "check", Status: "leased", LeaseUntil: &until}, Token: strings.Repeat("a", 64)}
	if !validPulledLease(valid) {
		t.Fatal("valid check lease rejected")
	}
	for _, mutate := range []func(*Lease){
		func(l *Lease) { l.Job.Kind = "arbitrary_command" },
		func(l *Lease) { l.Job.Target = "unknown" },
		func(l *Lease) { l.Job.Status = "completed" },
		func(l *Lease) { l.Job.LeaseUntil = nil },
		func(l *Lease) { past := time.Now().Add(-time.Minute); l.Job.LeaseUntil = &past },
		func(l *Lease) { l.Token = strings.Repeat("z", 64) },
		func(l *Lease) { l.Job.Kind = "apply" },
		func(l *Lease) { l.Job.Kind = "apply"; l.Job.Version = "2026.9.1"; l.Job.Evidence = "unverified" },
	} {
		l := valid
		mutate(&l)
		if validPulledLease(l) {
			t.Fatalf("invalid lease accepted: %+v", l.Job)
		}
	}
}
