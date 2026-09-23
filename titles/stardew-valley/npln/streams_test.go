package npln

import (
	"context"
	"sync"

	"google.golang.org/grpc/metadata"
)

// fakeStream is a server stream that records what a handler sends.
type fakeStream[T any] struct {
	ctx    context.Context
	mu     sync.Mutex
	sent   []*T
	notify chan struct{}
}

func newFakeStream[T any](ctx context.Context) *fakeStream[T] {
	return &fakeStream[T]{ctx: ctx, notify: make(chan struct{}, 64)}
}

func (f *fakeStream[T]) Send(m *T) error {
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	select {
	case f.notify <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeStream[T]) got() []*T {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*T(nil), f.sent...)
}

func (f *fakeStream[T]) Context() context.Context     { return f.ctx }
func (f *fakeStream[T]) SetHeader(metadata.MD) error  { return nil }
func (f *fakeStream[T]) SendHeader(metadata.MD) error { return nil }
func (f *fakeStream[T]) SetTrailer(metadata.MD)       {}
func (f *fakeStream[T]) SendMsg(any) error            { return nil }
func (f *fakeStream[T]) RecvMsg(any) error            { return nil }
