// Package probe measures how much each unit would reclaim, without deleting
// anything.
//
// Probing is du-bound and every unit is independent, so it runs on a worker
// pool. The bash version measured units one at a time, which is why a dry run
// on a developer machine took minutes.
package probe

import (
	"runtime"
	"sync"

	"github.com/mralaminahamed/reclaim/internal/fsutil"
	"github.com/mralaminahamed/reclaim/internal/unit"
)

// All measures every unit in r using at most workers goroutines. Passing 0 or
// less picks a default from the machine's CPU count.
//
// Nothing in this package writes to the filesystem. Each worker owns a distinct
// unit, so the shared registry is only read.
func All(r *unit.Registry, workers int) {
	units := r.All()
	if len(units) == 0 {
		return
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > len(units) {
		workers = len(units)
	}

	ch := make(chan *unit.Unit)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range ch {
				measure(u)
			}
		}()
	}
	for _, u := range units {
		ch <- u
	}
	close(ch)
	wg.Wait()
}

// measure fills in Bytes and Mount for a single unit.
func measure(u *unit.Unit) {
	if u.Kind == unit.KindCmd {
		// A command's yield is unknown until it runs; it is reported as it
		// happens rather than predicted here.
		u.Bytes = 0
		u.Mount = fsutil.MountOf(mountHint(u))
		return
	}

	var total int64
	for _, p := range u.Paths {
		if p == "" {
			continue
		}
		n, err := fsutil.PathBytes(p)
		if err != nil {
			continue
		}
		if u.Mount == "" {
			u.Mount = fsutil.MountOf(p)
		}
		total += n
	}
	if u.Mount == "" {
		u.Mount = fsutil.MountOf(mountHint(u))
	}
	u.Bytes = total
}

func mountHint(u *unit.Unit) string {
	if u.MountHint != "" {
		return u.MountHint
	}
	return homeDir()
}
