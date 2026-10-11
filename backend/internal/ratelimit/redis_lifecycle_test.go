package ratelimit

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRedisLimiterClosesOwnedClientExactlyOnceAndRetainsFailure(t *testing.T) {
	failure := errors.New("synthetic Redis close error")
	var calls atomic.Int32
	r := &RedisLimiter{closeClient: func() error { calls.Add(1); return failure }}
	var closers sync.WaitGroup
	for i := 0; i < 8; i++ {
		closers.Add(1)
		go func() {
			defer closers.Done()
			if err := r.Close(); !errors.Is(err, failure) {
				t.Errorf("close error lost: %v", err)
			}
		}()
	}
	closers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("client closed %d times", calls.Load())
	}
	if err := (&RedisLimiter{}).Close(); err != nil {
		t.Fatal(err)
	}
}
