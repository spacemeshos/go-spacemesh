package book_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

const testLimit = 4

func newTestState(tb testing.TB) *testState {
	return &testState{
		TB:    tb,
		book:  book.New(book.WithLimit(testLimit), book.WithRand(0)),
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
		expect := []book.Address{}
		for _, id := range ids {
			addr := ts.state[id]
			require.NotEmpty(ts, addr, "id=%s", id)
			expect = append(expect, addr)
		}
		require.Equal(ts, expect, rst, "share")
	}
}

func shareExpectNil(src book.ID, n int) step {
	return func(ts *testState) {
		require.Nil(ts, ts.book.TakeShareable(src, n))
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
			add("1", "/0.0.0.0/7777"),
			add("2", "/0.0.0.0/8888"),
			add("3", "/0.0.0.0/9999"),
			drain(3, "1", "2", "3"),
			update("1", book.Success),
			update("2", book.Fail),
			drain(3, "1", "2"),
			update("1", book.Success),
			update("2", book.Fail),
			share("3", 3, "1"),
		}},
		{"share is random", []step{
			add("1", "/0.0.0.0/6666"),
			add("2", "/0.0.0.0/7777"),
			add("3", "/0.0.0.0/8888"),
			add("4", "/0.0.0.0/9999"),
			repeat(
				2,
				drain(4, "1", "2", "3", "4"),
				update("1", book.Success),
				update("2", book.Success),
				update("3", book.Success),
				update("4", book.Success),
			),
			share("3", 2, "2", "1"),
			share("3", 2, "1", "4"),
		}},
		{"share skips source id", []step{
			add("1", "/0.0.0.0/6666"),
			add("2", "/0.0.0.0/7777"),
			add("3", "/0.0.0.0/8888"),
			repeat(
				2,
				drain(4, "1", "2", "3"),
				update("1", book.Success),
				update("2", book.Success),
				update("3", book.Success),
			),
			share("3", 3, "2", "1"),
		}},
		{"protected is shareable", []step{
			add("1", "/0.0.0.0/6666"),
			add("2", "/0.0.0.0/7777"),
			update("1", book.Protect),
			share("2", 3, "1"),
			repeat(10,
				drain(2, "1", "2"),
				update("1", book.Fail),
				update("2", book.Success),
			),
			share("2", 3, "1"),
		}},
		{"unknown is not shared", []step{
			add("1", "/0.0.0.0/6666"),
			add("2", "/0.0.0.0/7777"),
			share("2", 3),
		}},
		{"unknown is deleted after min failres", []step{
			add("1", "/0.0.0.0/6666"),
			repeat(
				2,
				drain(1, "1"),
				update("1", book.Fail),
			),
			drain(1),
		}},
		{"not added from unknown source", []step{
			addFrom("xx", "1", "/0.0.0.0/6666"),
			drain(1),
		}},
		{"added from known source", []step{
			add("xx", "/0.0.0.0/1111"),
			drain(1, "xx"),
			addFrom("xx", "1", "/0.0.0.0/6666"),
			drain(1, "1"),
		}},
		{"not added after limit is reached", []step{
			add("1", "/0.0.0.0/1111"),
			add("2", "/0.0.0.0/2222"),
			add("3", "/0.0.0.0/3333"),
			add("4", "/0.0.0.0/4444"),
			add("5", "/0.0.0.0/5555"),
			drain(5, "1", "2", "3", "4"),
		}},
		{"updated address is not shared", []step{
			add("1", "/0.0.0.0/1111"),
			add("2", "/0.0.0.0/2222"),
			repeat(
				2,
				drain(2, "1", "2"),
				update("1", book.Success),
				update("2", book.Success),
			),
			share("2", 1, "1"),
			add("1", "/0.0.0.0/1112"),
			share("2", 1),
		}},
		{"update for nil address is noop", []step{
			update("1", book.Protect),
		}},
		{"connect disconnect", []step{
			add("1", "/0.0.0.0/1112"),
			update("1", book.Connected),
			update("1", book.Disconnected),
		}},
		{"take shareable from nil source", []step{
			add("1", "/0.0.0.0/1111"),
			add("2", "/0.0.0.0/2222"),
			repeat(
				2,
				drain(2, "1", "2"),
				update("1", book.Success),
				update("2", book.Success),
			),
			share("1", 2, "2"),
			share("2", 2, "1"),
			shareExpectNil("3", 2),
		}},
		{"deleted from stable", []step{
			add("1", "/0.0.0.0/1111"),
			repeat(
				2,
				drain(2, "1"),
				update("1", book.Success),
			),
			add("2", "/0.0.0.0/2222"),
			share("2", 1, "1"),
			repeat(
				2,
				drain(2, "1", "2"),
				update("1", book.Fail),
				update("2", book.Success),
			),
			share("2", 1),
			repeat(
				2,
				drain(2, "1", "2"),
				update("1", book.Fail),
				update("2", book.Success),
			),
			drain(2, "2"),
		}},
		{"downgrade from good to unknown takes time", []step{
			add("1", "/0.0.0.0/1111"),
			add("2", "/0.0.0.0/2222"),
			repeat(14,
				drain(2, "1", "2"),
				update("1", book.Success),
				update("2", book.Success),
			),
			share("2", 1, "1"),
			repeat(9,
				drain(2, "1", "2"),
				update("1", book.Fail),
				update("2", book.Success),
			),
			share("2", 1, "1"),
			drain(2, "1", "2"),
			update("1", book.Fail),
			update("2", book.Success),
			share("2", 1),
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
