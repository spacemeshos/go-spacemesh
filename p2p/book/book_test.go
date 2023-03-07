package book_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

const testLimit = 4

func newTestState(tb testing.TB) *testState {
	opts := []book.Opt{
		book.WithLimit(testLimit), book.WithRand(0),
	}
	return &testState{
		TB:    tb,
		opts:  opts,
		book:  book.New(opts...),
		state: map[book.ID]book.Address{},
	}
}

type testState struct {
	testing.TB
	opts      []book.Opt
	book      *book.Book
	state     map[book.ID]book.Address
	persisted []byte
}

type step func(ts *testState)

func add(id book.ID, addr string) step {
	return addFrom(book.SELF, id, addr)
}

func addFrom(src, id book.ID, addr string) step {
	return func(ts *testState) {
		maddr, err := ma.NewMultiaddr(addr)
		require.NoError(ts, err)
		ts.state[id] = maddr
		ts.book.Add(src, id, maddr)
	}
}

func update(id book.ID, events ...book.Event) step {
	return func(ts *testState) {
		ts.book.Update(id, events...)
	}
}

func drain(n int, ids ...book.ID) step {
	return func(ts *testState) {
		rst := ts.book.DrainQueue(n)
		for i, id := range ids {
			addr := ts.state[id]
			require.NotEmpty(ts, addr, "id=%s", id)
			require.Equal(ts, addr.String(), rst[i].String(), "i=%d", i)
		}
	}
}

func share(src book.ID, n int, ids ...book.ID) step {
	return func(ts *testState) {
		rst := ts.book.TakeShareable(src, n)
		for i, id := range ids {
			addr := ts.state[id]
			require.NotEmpty(ts, addr, "id=%s", id)
			require.Equal(ts, addr.String(), rst[i].String())
		}
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

func persist(expect string) step {
	return func(ts *testState) {
		w := bytes.NewBuffer(nil)
		require.NoError(ts, ts.book.Persist(w))
		require.Equal(ts, strings.TrimSpace(expect), w.String())
		ts.persisted = w.Bytes()
	}
}

func stats(s book.Stats) step {
	return func(ts *testState) {
		require.Equal(ts, s, ts.book.Stats())
	}
}

type unexpectedEof struct{}

func (unexpectedEof) Write([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func faultyPersist(message string) step {
	return func(ts *testState) {
		require.ErrorContains(ts, ts.book.Persist(unexpectedEof{}), message)
	}
}

func recover() step {
	return func(ts *testState) {
		b := book.New(ts.opts...)
		require.NoError(ts, b.Recover(bytes.NewReader(ts.persisted)))
		ts.book = b
	}
}

func faultyRecover(content, message string) step {
	return func(ts *testState) {
		require.ErrorContains(ts, ts.book.Recover(bytes.NewReader([]byte(strings.TrimSpace(content)))), message)
	}
}

func TestBook(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		steps []step
	}{
		{"sanity", []step{
			add("1", "/ip4/0.0.0.0/tcp/7777"),
			add("2", "/ip4/0.0.0.0/tcp/8888"),
			add("3", "/ip4/0.0.0.0/tcp/9999"),
			drain(3, "1", "2", "3"),
			update("1", book.Success),
			update("2", book.Fail),
			drain(3, "1", "2"),
			update("1", book.Success),
			update("2", book.Fail),
			share("3", 3, "1"),
		}},
		{"share is random", []step{
			add("1", "/ip4/0.0.0.0/tcp/6666"),
			add("2", "/ip4/0.0.0.0/tcp/7777"),
			add("3", "/ip4/0.0.0.0/tcp/8888"),
			add("4", "/ip4/0.0.0.0/tcp/9999"),
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
			add("1", "/ip4/0.0.0.0/tcp/6666"),
			add("2", "/ip4/0.0.0.0/tcp/7777"),
			add("3", "/ip4/0.0.0.0/tcp/8888"),
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
			add("1", "/ip4/0.0.0.0/tcp/6666"),
			add("2", "/ip4/0.0.0.0/tcp/7777"),
			update("1", book.Protect),
			share("2", 3, "1"),
			repeat(10,
				drain(2, "1", "2"),
				update("1", book.Fail),
				update("2", book.Success),
			),
			share("2", 3, "1"),
		}},
		{"share protected even if it got stale", []step{
			add("1", "/ip4/0.0.0.0/tcp/6666"),
			drain(1, "1"),
			update("1", book.Fail),
			add("2", "/ip4/0.0.0.0/tcp/7777"),
			share("2", 1),
			update("1", book.Protect),
			share("2", 1, "1"),
		}},
		{"unknown is not shared", []step{
			add("1", "/ip4/0.0.0.0/tcp/6666"),
			add("2", "/ip4/0.0.0.0/tcp/7777"),
			repeat(2, update("1", book.Fail)),
			share("2", 3),
		}},
		{"learned is deleted after min failres", []step{
			add("1", "/ip4/0.0.0.0/tcp/6666"),
			repeat(3,
				drain(1, "1"),
				update("1", book.Fail),
			),
			drain(1),
		}},
		{"stale can get back to stable", []step{
			add("1", "/ip4/0.0.0.0/tcp/6666"),
			add("2", "/ip4/0.0.0.0/tcp/7777"),
			repeat(1,
				drain(1, "1"),
				update("1", book.Fail),
			),
			share("2", 1),
			repeat(2,
				drain(2, "2", "1"),
				update("2", book.Success),
				update("1", book.Success),
			),
			share("2", 1, "1"),
		}},
		{"not added from unknown source", []step{
			addFrom("xx", "1", "/ip4/0.0.0.0/tcp/6666"),
			drain(1),
		}},
		{"added from known source", []step{
			add("xx", "/ip4/0.0.0.0/tcp/1111"),
			drain(1, "xx"),
			addFrom("xx", "1", "/ip4/0.0.0.0/tcp/6666"),
			drain(1, "1"),
		}},
		{"dns is public", []step{
			add("1", "/dns4/fqdn/tcp/1111"),
			add("2", "/ip4/8.8.8.8/tcp/1111"),
			share("1", 1, "2"),
			share("2", 1, "1"),
		}},
		{"public and private are not shared with each other", []step{
			add("pub", "/ip4/8.8.8.8/tcp/1111"),
			add("private", "/ip4/192.168.0.1/tcp/1111"),
			share("pub", 1),
			share("private", 1),
		}},
		{"private is not added from a public source", []step{
			add("pub", "/ip4/8.8.8.8/tcp/1111"),
			addFrom("pub", "private", "/ip4/192.168.0.1/tcp/1111"),
			persist(`
{"id":"pub","raw":"/ip4/8.8.8.8/tcp/1111","class":2,"connected":false}
949218308665536579
`),
		}},
		{"public is not added from a private source", []step{
			add("private", "/ip4/192.168.0.1/tcp/1111"),
			addFrom("private", "pub", "/ip4/8.8.8.8/tcp/1111"),
			persist(`
{"id":"private","raw":"/ip4/192.168.0.1/tcp/1111","class":2,"connected":false}
11883410850220296542
`),
		}},
		{"not added after limit is reached", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			add("2", "/ip4/0.0.0.0/tcp/2222"),
			add("3", "/ip4/0.0.0.0/tcp/3333"),
			add("4", "/ip4/0.0.0.0/tcp/4444"),
			add("5", "/ip4/0.0.0.0/tcp/5555"),
			drain(5, "1", "2", "3", "4"),
		}},
		{"updated address preserves its state", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			add("2", "/ip4/0.0.0.0/tcp/2222"),
			repeat(
				2,
				drain(2, "1", "2"),
				update("1", book.Success),
				update("2", book.Fail),
			),
			share("2", 1, "1"),
			share("1", 1),
			add("1", "/ip4/0.0.0.0/tcp/1112"),
			add("2", "/ip4/0.0.0.0/tcp/2221"),
			share("2", 1, "1"),
			share("1", 1),
		}},
		{"update for nil address is noop", []step{
			update("1", book.Protect),
		}},
		{"connect disconnect", []step{
			add("1", "/ip4/0.0.0.0/tcp/1112"),
			update("1", book.Connected),
			persist(`
{"id":"1","raw":"/ip4/0.0.0.0/tcp/1112","class":2,"connected":true}
6601807895792160526
`),
			update("1", book.Disconnected),
			persist(`
{"id":"1","raw":"/ip4/0.0.0.0/tcp/1112","class":2,"connected":false}
7588170424014456815
`),
		}},
		{"take shareable from nil source", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			add("2", "/ip4/0.0.0.0/tcp/2222"),
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
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			repeat(
				2,
				drain(2, "1"),
				update("1", book.Success),
			),
			add("2", "/ip4/0.0.0.0/tcp/2222"),
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
		{"persist nothing", []step{
			persist("0"),
		}},
		{"recover addresses", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			add("2", "/ip4/0.0.0.0/tcp/2222"),
			repeat(2,
				persist(`
{"id":"1","raw":"/ip4/0.0.0.0/tcp/1111","class":2,"connected":false}
{"id":"2","raw":"/ip4/0.0.0.0/tcp/2222","class":2,"connected":false}
8805148713257375839
`),
				recover(),
			),
		}},
		{"sort on recovery", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			add("2", "/ip4/0.0.0.0/tcp/2222"),
			update("2", book.Connected),
			persist(
				`
{"id":"1","raw":"/ip4/0.0.0.0/tcp/1111","class":2,"connected":false}
{"id":"2","raw":"/ip4/0.0.0.0/tcp/2222","class":2,"connected":true}
516371166648090384
`),
			recover(),
			drain(2, "2", "1"),
		}},
		{"persist error on checksum", []step{
			faultyPersist("write checksum"),
		}},
		{"persist json encoder", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			faultyPersist("json encoder"),
		}},
		{"recovery fails", []step{
			faultyRecover(`
{"id":"1","raw":"","class":2,"connected":false`,
				"unmarshal"),
			faultyRecover(`
{"id":"1","raw":10,"class":2,"connected":false}`,
				"addressInfo.raw"),
			faultyRecover(`
{"id":"1","raw": "/invalid-addr/111","class":2,"connected":false}`,
				"unknown protocol invalid-addr"),
			faultyRecover(`
{"id":"1","raw":"/ip4/0.0.0.0/tcp/1111","class":2,"connected":false}

{"id":"2","raw":"/ip4/0.0.0.0/tcp/2222","class":2,"connected":false}
`,
				"empty lines are not expected"),
			faultyRecover(`xyz`, "parse uint xyz"),
			faultyRecover(`
{"id":"1","raw":"/ip4/0.0.0.0/tcp/1111","class":2,"connected":false}
{"id":"2","raw":"/ip4/0.0.0.0/tcp/2222","class":2,"connected":false}
111`, "stored checksum 111"),
		}},
		{"internal state not corrupted on recovery failure", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			faultyRecover(`
{"id":"2","raw":"/ip4/0.0.0.0/tcp/2222","class":2,"connected":false}
111			
`, "checksum"),
			persist(`
{"id":"1","raw":"/ip4/0.0.0.0/tcp/1111","class":2,"connected":false}
4927299508238403564
`),
		}},
		{"stats", []step{
			add("1", "/ip4/0.0.0.0/tcp/1111"),
			stats(book.Stats{Total: 1, Private: 1, Learned: 1}),
			add("2", "/ip4/8.8.8.8/tcp/1111"),
			stats(book.Stats{Total: 2, Public: 1, Private: 1, Learned: 2}),
			update("1", book.Fail),
			update("2", book.Connected, book.Success),
			stats(book.Stats{Total: 2, Public: 1, Private: 1, Connected: 1, Learned: 1, Stale: 1}),
			update("2", book.Success),
			stats(book.Stats{Total: 2, Public: 1, Private: 1, Connected: 1, Stable: 1, Stale: 1}),
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
