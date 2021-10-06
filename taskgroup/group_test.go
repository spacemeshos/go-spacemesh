package taskgroup

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupTerminate(t *testing.T) {
	t.Run("OnError", func(t *testing.T) {
		testErr := errors.New("test")
		g := New()
		for i := 0; i < 10; i++ {
			g.Go(func(ctx context.Context) error {
				<-ctx.Done()
				return fmt.Errorf("context done: %w", ctx.Err())
			})
		}
		g.Go(func(ctx context.Context) error {
			return testErr
		})
		require.ErrorIs(t, g.Wait(), testErr)
	})
	t.Run("OnCancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		g := New(WithContext(ctx))
		for i := 0; i < 10; i++ {
			g.Go(func(ctx context.Context) error {
				<-ctx.Done()
				return fmt.Errorf("context done: %w", ctx.Err())
			})
		}
		cancel()
		require.ErrorIs(t, g.Wait(), context.Canceled)
	})
}

func TestGroupAfterWait(t *testing.T) {
	testErr := errors.New("test")
	g := New()
	g.Go(func(ctx context.Context) error {
		return testErr
	})
	require.ErrorIs(t, g.Wait(), testErr)
	require.ErrorIs(t, g.Go(func(ctx context.Context) error {
		return nil
	}), ErrTerminated)
}

func TestGroupNested(t *testing.T) {
	g := New()
	testErr := errors.New("test")
	g.Go(func(ctx context.Context) error {
		for i := 0; i < 10; i++ {
			g.Go(func(ctx context.Context) error {
				<-ctx.Done()
				return fmt.Errorf("context done: %w", ctx.Err())
			})
			g.Go(func(ctx context.Context) error {
				return testErr
			})
		}
		<-ctx.Done()
		return fmt.Errorf("context done: %w", ctx.Err())
	})

	require.ErrorIs(t, g.Wait(), testErr)
}

func TestGroupDoubleWait(t *testing.T) {
	var (
		testErr  = errors.New("test")
		g        = New()
		n        = 10
		waitErrs = make(chan error, n)
		wg       sync.WaitGroup
	)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			waitErrs <- g.Wait()
			wg.Done()
		}()
	}

	g.Go(func(ctx context.Context) error {
		return testErr
	})
	wg.Wait()
	close(waitErrs)
	for err := range waitErrs {
		require.ErrorIs(t, err, testErr)
	}
}
