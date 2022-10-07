package utils

import (
	"fmt"
	"sync"
	"time"
)

// wait for a WaitGroup to finish or timeout
func WaitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("Timed out waiting for wait group to finish")
	}
}
