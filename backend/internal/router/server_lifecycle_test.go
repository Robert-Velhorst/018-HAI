package router

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

type shutdownTestListener struct {
	net.Listener
	closed      chan struct{}
	once        sync.Once
	failure     chan struct{}
	failureOnce sync.Once
	accepted    bool
}

var errShutdownTestAccept = errors.New("injected permanent listener failure")

func (l *shutdownTestListener) Accept() (net.Conn, error) {
	if l.failure == nil || !l.accepted {
		conn, err := l.Listener.Accept()
		l.accepted = err == nil
		return conn, err
	}
	<-l.failure
	return nil, errShutdownTestAccept
}

func (l *shutdownTestListener) fail() {
	if l.failure != nil {
		l.failureOnce.Do(func() { close(l.failure) })
	}
}

func (l *shutdownTestListener) Close() error {
	err := l.Listener.Close()
	l.fail()
	l.once.Do(func() { close(l.closed) })
	return err
}

func TestServeWithContextWaitsForDrainAndReportsTimeout(t *testing.T) {
	for _, name := range []string{"drains_request", "timeout_closes_request", "listener_failure_drains_request"} {
		t.Run(name, func(t *testing.T) {
			forceTimeout := name == "timeout_closes_request"
			listenerFailure := name == "listener_failure_drains_request"
			base, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			listener := &shutdownTestListener{Listener: base, closed: make(chan struct{})}
			if listenerFailure {
				listener.failure = make(chan struct{})
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			handlerDone := make(chan struct{})
			var releaseOnce sync.Once
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				close(entered)
				select {
				case <-release:
					w.WriteHeader(http.StatusNoContent)
				case <-r.Context().Done():
				}
			})}
			ctx, cancel := context.WithCancel(context.Background())
			timeout := 3 * time.Second
			if forceTimeout {
				timeout = 100 * time.Millisecond
			}
			serveResult := make(chan error, 1)
			serveJoined := make(chan struct{})
			go func() {
				defer close(serveJoined)
				serveResult <- serveWithShutdownTimeout(ctx, server, listener, timeout)
			}()
			transport := &http.Transport{DisableKeepAlives: true}
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			type requestOutcome struct {
				status int
				err    error
			}
			requestResult := make(chan requestOutcome, 1)
			requestJoined := make(chan struct{})
			go func() {
				defer close(requestJoined)
				response, requestErr := client.Get("http://" + base.Addr().String())
				outcome := requestOutcome{err: requestErr}
				if response != nil {
					outcome.status = response.StatusCode
					response.Body.Close()
				}
				requestResult <- outcome
			}()
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(release) })
				cancel()
				server.Close()
				listener.Close()
				transport.CloseIdleConnections()
				for _, joined := range []<-chan struct{}{serveJoined, requestJoined} {
					select {
					case <-joined:
					case <-time.After(6 * time.Second):
						t.Error("owned HTTP test goroutine failed to terminate")
					}
				}
				select {
				case <-entered:
					select {
					case <-handlerDone:
					case <-time.After(time.Second):
						t.Error("entered test handler failed to terminate during cleanup")
					}
				default:
				}
			})
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("request never reached the handler")
			}
			if listenerFailure {
				listener.fail()
			} else {
				cancel()
			}
			select {
			case <-listener.closed:
			case <-time.After(time.Second):
				t.Fatal("shutdown did not close the listener")
			}
			if !forceTimeout {
				select {
				case err := <-serveResult:
					t.Fatalf("returned before the active request completed: %v", err)
				case <-time.After(40 * time.Millisecond):
				}
				releaseOnce.Do(func() { close(release) })
			}
			select {
			case err := <-serveResult:
				if forceTimeout {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("shutdown error = %v, want deadline exceeded", err)
					}
				} else if listenerFailure {
					if !errors.Is(err, errShutdownTestAccept) {
						t.Fatalf("serve error = %v, want original listener failure", err)
					}
				} else if err != nil {
					t.Fatalf("graceful shutdown failed: %v", err)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("server failed to finish bounded shutdown")
			}
			select {
			case outcome := <-requestResult:
				if forceTimeout && outcome.err == nil {
					t.Fatal("request unexpectedly completed after forced shutdown")
				}
				if !forceTimeout && (outcome.err != nil || outcome.status != http.StatusNoContent) {
					t.Fatalf("drained request: status=%d error=%v, want 204 without error", outcome.status, outcome.err)
				}
			case <-time.After(time.Second):
				t.Fatal("request did not terminate")
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("handler did not terminate")
			}
		})
	}
}

func TestServeWithContextPreservesListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	if err := serveWithContext(context.Background(), &http.Server{}, listener); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("serve error = %v, want closed listener", err)
	}
}

func TestServeWithShutdownTimeoutRequiresServerAndPositiveTimeout(t *testing.T) {
	if err := serveWithContext(nil, nil, nil); err == nil {
		t.Fatal("nil server accepted")
	}
	if err := serveWithShutdownTimeout(nil, &http.Server{}, nil, 0); err == nil {
		t.Fatal("zero shutdown timeout accepted")
	}
}

func TestServeWithContextStopsOnCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	server := &http.Server{Handler: http.NewServeMux()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveWithContext(ctx, server, listener)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveWithContext returned %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serveWithContext did not stop after context cancellation")
	}

	if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("server close: %v", err)
	}
}

func TestHTTPServerUsesBoundedUploadCompatibleTimeouts(t *testing.T) {
	server := newHTTPServer(":8080", http.NewServeMux())
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != maxRequestReadDuration {
		t.Fatalf("ReadTimeout = %v, want %v", server.ReadTimeout, maxRequestReadDuration)
	}
	if server.WriteTimeout != maxUploadWriteDuration {
		t.Fatalf("WriteTimeout = %v, want %v", server.WriteTimeout, maxUploadWriteDuration)
	}
	if server.IdleTimeout != defaultIdleConnectionTTL {
		t.Fatalf("IdleTimeout = %v, want %v", server.IdleTimeout, defaultIdleConnectionTTL)
	}
}

func TestShutdownSignalsIncludeInterrupt(t *testing.T) {
	for _, candidate := range shutdownSignals() {
		if candidate == os.Interrupt {
			return
		}
	}
	t.Fatal("shutdown signals must include os.Interrupt")
}
