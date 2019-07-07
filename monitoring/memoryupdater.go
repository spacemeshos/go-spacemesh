package monitoring

import (
	"fmt"
	"runtime"
)

var names = []string{"NumGoroutine", "Alloc", "TotalAlloc", "Sys", "Mallocs", "Frees", "LiveObjects", "PauseTotalNs", "NumGC"}

type MemoryUpdater struct {
	memTracker   map[string]*Tracker
	recordsCount int
}

func NewMemoryUpdater() *MemoryUpdater {
	mu := new(MemoryUpdater)
	mu.memTracker = make(map[string]*Tracker)
	for _, name := range names {
		mu.memTracker[name] = NewTracker()
	}

	return mu
}

func (mu *MemoryUpdater) Update() {
	var rtm runtime.MemStats

	// Read full mem stats
	runtime.ReadMemStats(&rtm)

	// Number of goroutines )
	mu.memTracker["NumGoroutine"].Track(uint64(runtime.NumGoroutine()))

	// Misc memory stats
	mu.memTracker["Alloc"].Track(rtm.Alloc)
	mu.memTracker["TotalAlloc"].Track(rtm.TotalAlloc)
	mu.memTracker["Sys"].Track(rtm.Sys)
	mu.memTracker["Mallocs"].Track(rtm.Mallocs)
	mu.memTracker["Frees"].Track(rtm.Frees)

	// Live objects = Mallocs - Frees
	mu.memTracker["LiveObjects"].Track(rtm.Mallocs - rtm.Frees)

	// GC Stats
	mu.memTracker["PauseTotalNs"].Track(rtm.PauseTotalNs)
	mu.memTracker["NumGC"].Track(uint64(rtm.NumGC))

	mu.recordsCount++
}

// Status - returns a string description of the current status
func (mu *MemoryUpdater) Status() string {
	s := fmt.Sprintf("Records count=%v\n", mu.recordsCount)

	for _, name := range names {
		s += fmt.Sprintf("Name=%s\t\t\tMax=%v\t\tMin=%v\t\tAvg=%v\n",
			name,
			mu.memTracker[name].Max(),
			mu.memTracker[name].Min(),
			mu.memTracker[name].Avg())
	}

	return s
}
