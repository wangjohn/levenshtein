package verify

import (
	"context"
	"path/filepath"
	"sync"
)

// Native checks all run in the consumer's working tree, so they coordinate at
// two levels. Within this process a per-source gate lets any number of
// read-only checks overlap while a check that can write the tree runs alone.
// Across processes the gate's holders share one file lock, held while anyone
// in this process is inside the tree, so another CLI on the same source waits
// for all of them. An flock is per open file, and a second handle in this
// process would block exactly as another process does, so the process takes
// it once rather than once per check.

// readOnlyKinds are the native kinds that never write the source tree: the
// shared Go kinds analyze it in place or work on a scratch copy, and
// semantic-lint only reads git history. A command check, with or without
// stages, is the only kind that writes.
//
// TODO(WS-C): fold this into the per-kind descriptor.
func readOnlyKind(kind CheckKind) bool {
	return sharedGoChecks[kind] || kind == CheckSemanticLint
}

// writesWorkspace reports whether executing a native check can change the
// working tree, and so must exclude every other native check.
func writesWorkspace(req Request) bool {
	return !readOnlyKind(req.Check.Kind) || len(req.stages()) > 0
}

// gate is an in-process reader/writer lock whose waits a context can abandon.
// A queued writer holds back new readers, so a steady stream of read-only
// checks cannot starve a command check.
type gate struct {
	mu      sync.Mutex
	readers int
	writer  bool
	queued  int
	wake    chan struct{}
}

func (g *gate) acquire(ctx context.Context, name string, exclusive bool) (func(), error) {
	g.mu.Lock()
	if exclusive {
		g.queued++
	}
	waited := false
	for {
		if exclusive && !g.writer && g.readers == 0 {
			g.queued--
			g.writer = true
			break
		}
		if !exclusive && !g.writer && g.queued == 0 {
			g.readers++
			break
		}

		if g.wake == nil {
			g.wake = make(chan struct{})
		}
		wake := g.wake
		g.mu.Unlock()
		if !waited {
			waited = true
			waitingOn(name)
		}
		select {
		case <-wake:
		case <-ctx.Done():
			g.mu.Lock()
			if exclusive {
				g.queued--
				g.broadcast() // Readers held back for this writer may now enter.
			}
			g.mu.Unlock()
			return nil, ctx.Err()
		}
		g.mu.Lock()
	}
	g.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()

			if exclusive {
				g.writer = false
			} else {
				g.readers--
			}
			g.broadcast()
		})
	}, nil
}

// broadcast wakes every waiter to re-check the state. The caller holds g.mu.
func (g *gate) broadcast() {
	if g.wake != nil {
		close(g.wake)
		g.wake = nil
	}
}

// fileHold shares one cross-process file lock among this process's holders.
// slot serializes taking and dropping it, and a context can abandon the wait
// for the slot as well as the wait for the lock.
type fileHold struct {
	slot    chan struct{}
	holders int
	unlock  func()
}

func (h *fileHold) acquire(ctx context.Context, path string) (func(), error) {
	select {
	case h.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-h.slot }()

	if h.holders == 0 {
		unlock, err := lockFile(ctx, path)
		if err != nil {
			return nil, err
		}
		h.unlock = unlock
	}
	h.holders++

	var once sync.Once
	return func() {
		once.Do(func() {
			h.slot <- struct{}{}
			defer func() { <-h.slot }()

			h.holders--
			if h.holders == 0 {
				h.unlock()
				h.unlock = nil
			}
		})
	}, nil
}

var (
	gates sync.Map // source → *gate
	holds sync.Map // lock path → *fileHold
)

// acquireWorkspace enters source's working tree for one native check, shared
// or exclusive, and returns the function that leaves it. dir is the cache
// directory whose locks coordinate with other processes; without one only
// this process is coordinated, as there is nowhere shared to put a lock.
func acquireWorkspace(ctx context.Context, dir, source string, exclusive bool) (func(), error) {
	value, _ := gates.LoadOrStore(source, &gate{})
	leave, err := value.(*gate).acquire(ctx, "gate:"+source, exclusive)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return leave, nil
	}

	path := filepath.Join(dir, "locks", "workspace-"+digest(source))
	value, _ = holds.LoadOrStore(path, &fileHold{slot: make(chan struct{}, 1)})
	release, err := value.(*fileHold).acquire(ctx, path)
	if err != nil {
		leave()
		return nil, err
	}
	return func() {
		release()
		leave()
	}, nil
}
