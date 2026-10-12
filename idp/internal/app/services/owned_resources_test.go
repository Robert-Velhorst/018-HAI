package services

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

type resourceCloseFunc func() error

func (f resourceCloseFunc) Close() error { return f() }

func TestOwnedResourcesReverseOrderAndErrors(t *testing.T) {
	first := errors.New("first close failed")
	second := errors.New("second close failed")
	var order []int
	r := &OwnedResources{}
	r.Add(nil)
	r.Add(struct{}{})
	r.Add(resourceCloseFunc(func() error { order = append(order, 1); return first }))
	r.Add(resourceCloseFunc(func() error { order = append(order, 2); return second }))
	err := r.Close()
	if !reflect.DeepEqual(order, []int{2, 1}) || !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("order=%v errors=%v", order, err)
	}
	if again := r.Close(); again != err || len(order) != 2 {
		t.Fatal("cleanup repeated or lost error")
	}
}

func TestOwnedResourcesConcurrentCloseOnce(t *testing.T) {
	var count atomic.Int32
	errClose := errors.New("close failed")
	r := &OwnedResources{}
	r.Add(resourceCloseFunc(func() error { count.Add(1); return errClose }))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !errors.Is(r.Close(), errClose) {
				t.Error("close error lost")
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("closed %d times", count.Load())
	}
	var absent *OwnedResources
	if absent.Close() != nil {
		t.Error("nil owner close failed")
	}
}
