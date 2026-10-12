package services

import (
	"errors"
	"io"
	"sync"
)

// OwnedResources is populated during serial startup, then closed once.
type OwnedResources struct {
	closers []io.Closer
	once    sync.Once
	err     error
}

func (r *OwnedResources) Add(resource any) {
	if closer, ok := resource.(io.Closer); ok {
		r.closers = append(r.closers, closer)
	}
}

func (r *OwnedResources) Close() error {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		for i := len(r.closers) - 1; i >= 0; i-- {
			r.err = errors.Join(r.err, r.closers[i].Close())
		}
	})
	return r.err
}
