package book_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

const testLimit = 3

func newTestState(tb testing.TB) *testState {
	return &testState{
		TB:    tb,
		book:  book.New(book.WithLimit(testLimit)),
		state: map[book.ID]book.Address{},
	}
}

type testState struct {
	testing.TB
	book  *book.Book
	state map[book.ID]book.Address
}

type step func(ts *testState)

func add(id book.ID, addr book.Address) step {
	return addFrom(book.SELF, id, addr)
}

func addFrom(src, id book.ID, addr book.Address) step {
	return func(ts *testState) {
		ts.state[id] = addr
		ts.book.Add(src, id, addr)
	}
}

func update(id book.ID, event book.Event) step {
	return func(ts *testState) {
		ts.book.Update(id, event)
	}
}

func drain(n int, ids ...book.ID) step {
	return func(ts *testState) {
		rst := ts.book.DrainQueue(n)
		if len(ids) == 0 {
			return
		}
		expect := []book.Address{}
		for _, id := range ids {
			addr := ts.state[id]
			require.NotEmpty(ts, addr, "id=%s", id)
			expect = append(expect, addr)
		}
		require.Equal(ts, expect, rst, "drain queue")
	}
}

func share(src book.ID, n int, ids ...book.ID) step {
	return func(ts *testState) {
		rst := ts.book.TakeShareable(src, n)
		if len(ids) == 0 {
			return
		}
		expect := []book.Address{}
		for _, id := range ids {
			addr := ts.state[id]
			require.NotEmpty(ts, addr, "id=%s", id)
			expect = append(expect, addr)
		}
		require.Equal(ts, expect, rst, "share")
	}
}

func repeat(n int, steps ...step) step {
	return func(ts *testState) {
		for i := 0; i < n; i++ {
			for _, step := range steps {
				step(ts)
			}
		}
	}
}

func TestBook(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		steps []step
	}{
		{"sanity", []step{
			add("10", "/0.0.0.0/7777"),
			add("20", "/0.0.0.0/8888"),
			add("30", "/0.0.0.0/9999"),
			drain(3, "10", "20", "30"),
			update("10", book.Success),
			update("20", book.Fail),
			drain(3, "10", "20"),
			update("10", book.Success),
			update("20", book.Fail),
			share("30", 3, "10"),
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			state := newTestState(t)
			for _, step := range tc.steps {
				step(state)
			}
		})
	}
}
