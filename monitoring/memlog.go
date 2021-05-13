package monitoring

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"runtime"
)

// MemLogger stores state related to simple memory logging
type MemLogger struct {
	Logger log.Log
}

// LogMemoryUsage logs a snapshot of current memory usage
func (ml MemLogger) LogMemoryUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	ml.Logger.With().Info("current memory profile",
		log.String("alloc", bytesToMBFormmater(m.Alloc)),
		log.String("total_alloc", bytesToMBFormmater(m.TotalAlloc)),
		log.String("sys", bytesToMBFormmater(m.Sys)),
		log.String("heap_alloc", bytesToMBFormmater(m.HeapAlloc)),
		log.Uint64("heap_alloc", m.HeapObjects))
}
