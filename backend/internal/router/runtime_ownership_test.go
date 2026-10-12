package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"github.com/gin-gonic/gin"
)

func TestRuntimeCleanupJoinsActualHTTPHandlerAndDetachedChild(t *testing.T) {
	g := lifecycle.New(context.Background())
	requestRelease, childRelease := make(chan struct{}), make(chan struct{})
	entered, requestDone := make(chan struct{}), make(chan struct{})
	var requestOnce, childOnce sync.Once
	var closes atomic.Int32
	closePool := func() error { closes.Add(1); return nil }
	defer func() {
		requestOnce.Do(func() { close(requestRelease) })
		childOnce.Do(func() { close(childRelease) })
		if err := finishRuntime(g, func() error { return nil }, time.Second); err != nil {
			t.Error(err)
		}
	}()
	r := gin.New()
	r.Use(runtimeOwnershipMiddleware(g))
	r.GET("/held", func(c *gin.Context) {
		if !lifecycle.Go(context.WithoutCancel(c.Request.Context()), "request-child", func() { <-childRelease }) {
			t.Error("request child not owned")
		}
		close(entered)
		<-requestRelease
		if c.Request.Context().Err() != nil {
			t.Error("group stop incorrectly canceled the caller context")
		}
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	go func() {
		r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/held", nil))
		close(requestDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	if err := finishRuntime(g, closePool, 15*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || closes.Load() != 0 {
		t.Fatalf("held request cleanup = %v, closes=%d", err, closes.Load())
	}
	late := httptest.NewRecorder()
	r.ServeHTTP(late, httptest.NewRequest(http.MethodGet, "/held", nil))
	if late.Code != http.StatusServiceUnavailable {
		t.Fatalf("new request after stop = %d", late.Code)
	}
	requestOnce.Do(func() { close(requestRelease) })
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("held response = %d", response.Code)
	}
	if err := finishRuntime(g, closePool, 15*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) || closes.Load() != 0 {
		t.Fatalf("detached child cleanup = %v, closes=%d", err, closes.Load())
	}
	childOnce.Do(func() { close(childRelease) })
	if err := finishRuntime(g, closePool, time.Second); err != nil || closes.Load() != 1 {
		t.Fatalf("drained cleanup = %v, closes=%d", err, closes.Load())
	}
}

func TestRuntimeCleanupPropagatesPoolFailureAndValidatesInputs(t *testing.T) {
	failure := errors.New("synthetic pool close failure")
	g := lifecycle.New(context.Background())
	startupFailure := errors.New("synthetic startup failure")
	result := errors.Join(startupFailure, finishRuntime(g, func() error { return failure }, time.Second))
	if !errors.Is(result, failure) || !errors.Is(result, startupFailure) {
		t.Fatalf("startup/cleanup failure lost: %v", result)
	}
	for _, tt := range []struct {
		group   *lifecycle.Group
		close   func() error
		timeout time.Duration
	}{
		{nil, func() error { return nil }, time.Second},
		{g, nil, time.Second},
		{g, func() error { return nil }, 0},
	} {
		if finishRuntime(tt.group, tt.close, tt.timeout) == nil {
			t.Fatal("invalid cleanup inputs accepted")
		}
	}
}
