package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/monitoring"
	"time"
)

func work(term chan struct{}) {
	m := 100
	for i := 1; i < 100; i++ {
		s := m * i
		a := make([]uint64, s, s)
		a[0] = 5
		time.Sleep(2 * time.Second)
		fmt.Printf("Done %v\n", i)
	}
	term <- struct{}{}
	term <- struct{}{}
}

func main() {
	term := make(chan struct{})
	u := monitoring.NewMemoryUpdater()
	m := monitoring.NewMonitor(1, u, term)
	m.Start()
	go work(term)
	<-term
	fmt.Println(u.Status())
}
