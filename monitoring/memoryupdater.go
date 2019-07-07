package monitoring

import (
	"fmt"
	"runtime"
)

const formatStr = "Name=%20s\t\tMax=%12v\t\tMin=%12v\t\tAvg=%12v\n"

var names = []string{"NumGoroutine", "Alloc", "TotalAlloc", "Sys", "Mallocs", "Frees", "LiveObjects", "PauseTotalNs", "NumGC"}

type formatter func(uint64) string

func bytesToMBFormmater(x uint64) string {
	return fmt.Sprintf("%vMB", x/1024/1024)
}

type MemoryUpdater struct {
	memTracker   map[string]*Tracker
	formatters   map[string]formatter
	recordsCount int
}

func NewMemoryUpdater() *MemoryUpdater {
	mu := new(MemoryUpdater)
	mu.memTracker = make(map[string]*Tracker)
	mu.formatters = make(map[string]formatter)
	for _, name := range names {
		mu.memTracker[name] = NewTracker()
		mu.formatters[name] = nil
	}

	mu.formatters["Alloc"] = bytesToMBFormmater
	mu.formatters["TotalAlloc"] = bytesToMBFormmater
	mu.formatters["Alloc"] = bytesToMBFormmater
	mu.formatters["Alloc"] = bytesToMBFormmater

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
		max := mu.memTracker[name].Max()
		min := mu.memTracker[name].Min()
		avg := uint64(mu.memTracker[name].Avg())

		format := mu.formatters[name]
		if format == nil {
			s += fmt.Sprintf(formatStr, name, max, min, avg)
		} else {
			s += fmt.Sprintf(formatStr, name, format(max), format(min), format(avg))
		}
	}

	return s
}
