package delimited

import (
	"testing"
	"time"
)

type mockc struct {
}

func (mockc) Write(b []byte) (int, error) {
	return len(b), nil
}

func (mockc) Read(p []byte) (n int, err error) {
	return len(p), nil
}

func (mockc) Close() error {
	return nil
}

func Test_Chan(t *testing.T) {
	c := NewChan(1000)
	m := mockc{}
	c.Pipe(m)

	done := make(chan struct{}, 2000)

	for i := 0; i < 2000; i++ {
		//i:=i
		go func() {
			_ = c.Out([]byte("LALAL"))
			done <- struct{}{}
		}()
	}

	c.Close()

	for i := 0; i < 2000; i++ {
		tx := time.NewTimer(10 * time.Second)
		select {
		case <-done:
			continue
		case <-tx.C:
			t.Fatal("timeout waiting for message")

		}
	}
}
