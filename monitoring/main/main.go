package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/monitoring"
	"time"
)

func work(term chan struct{}) {
	m := 100
	for i := 1; i < 3; i++ {
		s := m * i
		a := make([]uint64, s, s)
		a[0] = 5
		time.Sleep(2 * time.Second)
		fmt.Printf("Done %v\n", i)
		go func() { time.Sleep(5) }()
	}
	term <- struct{}{}
	term <- struct{}{}
}

func main() {
	term := make(chan struct{})
	u := monitoring.NewMemoryUpdater()
	m := monitoring.NewMonitor(1*time.Second, 1200*time.Millisecond, u, term)
	m.Start()
	go work(term)
	<-term
}
