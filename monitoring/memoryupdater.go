package monitoring

import (
	"fmt"
	"runtime"
)

var names = []string{"NumGoroutine", "Alloc", "TotalAlloc", "Sys", "Mallocs", "Frees", "LiveObjects", "PauseTotalNs", "NumGC"}

type MemoryUpdater struct {
	memTracker map[string]*Tracker
}

func NewMemoryUpdater() *MemoryUpdater {
	mu := new(MemoryUpdater)
	mu.memTracker = make(map[string]*Tracker)
	for _, name := range names {
		mu.memTracker[name] = NewTracker()
	}

	return mu
}

func (ma *MemoryUpdater) Update() {
	var rtm runtime.MemStats

	// Read full mem stats
	runtime.ReadMemStats(&rtm)

	// Number of goroutines )
	ma.memTracker["NumGoroutine"].Track(uint64(runtime.NumGoroutine()))

	// Misc memory stats
	ma.memTracker["Alloc"].Track(rtm.Alloc)
	ma.memTracker["TotalAlloc"].Track(rtm.TotalAlloc)
	ma.memTracker["Sys"].Track(rtm.Sys)
	ma.memTracker["Mallocs"].Track(rtm.Mallocs)
	ma.memTracker["Frees"].Track(rtm.Frees)

	// Live objects = Mallocs - Frees
	ma.memTracker["LiveObjects"].Track(rtm.Mallocs - rtm.Frees)

	// GC Stats
	ma.memTracker["PauseTotalNs"].Track(rtm.PauseTotalNs)
	ma.memTracker["NumGC"].Track(uint64(rtm.NumGC))
}

// Status - returns a string description of the current status
func (mu *MemoryUpdater) Status() string {
	s := ""
	for _, name := range names {
		s += fmt.Sprintf("Name=%s\tMax=%v\tMin=%v\tAvg=%v\n",
			name,
			mu.memTracker[name].Max(),
			mu.memTracker[name].Min(),
			mu.memTracker[name].Avg())
	}

	return s
}
