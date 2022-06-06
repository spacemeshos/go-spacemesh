package vm

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
)

func newTester(tb testing.TB) *tester {
	return &tester{
		TB:  tb,
		VM:  New(sql.InMemory(), WithLogger(logtest.New(tb))),
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

type tester struct {
	testing.TB
	*VM

	rng *rand.Rand

	pks       []ed25519.PrivateKey
	pubs      []ed25519.PublicKey
	addresses []core.Address
	nonces    []core.Nonce
}

func (t *tester) persistent() *tester {
	db, err := sql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	require.NoError(t, err)
	t.VM = New(db, WithLogger(logtest.New(t)))
	return t
}

func (t *tester) addAccounts(n int) *tester {
	for i := 0; i < n; i++ {
		pub, pk, err := ed25519.GenerateKey(t.rng)
		require.NoError(t, err)
		t.pks = append(t.pks, pk)
		t.pubs = append(t.pubs, pub)
		args := wallet.SpawnArguments{}
		copy(args.PublicKey[:], pub)
		t.addresses = append(t.addresses, core.ComputePrincipal(wallet.TemplateAddress, core.Nonce{}, &args))
		t.nonces = append(t.nonces, core.Nonce{})
	}
	return t
}

func (t *tester) applyGenesis() *tester {
	return t.applyGenesisWithBalance(1_000_000_000_000)
}

func (t *tester) applyGenesisWithBalance(amount uint64) *tester {
	accounts := make([]core.Account, len(t.pks))
	for i := range accounts {
		accounts[i] = core.Account{
			Address: types.Address(t.addresses[i]),
			Balance: amount,
		}
	}
	require.NoError(t, t.VM.ApplyGenesis(accounts))
	return t
}

func (t *tester) nextNonce(i int) core.Nonce {
	nonce := t.nonces[i]
	t.nonces[i].Counter++
	return nonce
}

func (t *tester) spawnWalletAll() [][]byte {
	var rst [][]byte
	for i := 0; i < len(t.addresses); i++ {
		if t.nonces[i].Counter != 0 {
			continue
		}
		rst = append(rst, t.selfSpawnWallet(i))
	}
	return rst
}

func (t *tester) selfSpawnWallet(i int) []byte {
	typ := scale.U8(0)
	method := scale.U8(0)
	payload := wallet.SpawnPayload{GasPrice: 1}
	copy(payload.Arguments.PublicKey[:], t.pubs[i])
	t.nextNonce(i)
	return encodeWalletTx(t, t.pks[i],
		&typ,
		&t.addresses[i], &method, &wallet.TemplateAddress,
		&payload,
	)
}

func (t *tester) randSendWalletN(n int, amount uint64) [][]byte {
	rst := make([][]byte, n)
	for i := range rst {
		rst[i] = t.randSpendWallet(amount)
	}
	return rst
}

func (t *tester) randSpendWallet(amount uint64) []byte {
	return t.spendWallet(t.rng.Intn(len(t.addresses)), t.rng.Intn(len(t.addresses)), amount)
}

func (t *tester) withSeed(seed int64) *tester {
	t.rng = rand.New(rand.NewSource(seed))
	return t
}

func (t *tester) spendWallet(from, to int, amount uint64) []byte {
	return t.spendWalletWithNonce(from, to, amount, t.nextNonce(from))
}

func (t *tester) spendWalletWithNonce(from, to int, amount uint64, nonce core.Nonce) []byte {
	payload := wallet.SpendPayload{}
	payload.Arguments.Destination = t.addresses[to]
	payload.Arguments.Amount = amount
	payload.GasPrice = 1
	payload.Nonce = nonce

	typ := scale.U8(0)
	method := scale.U8(1)
	return encodeWalletTx(t, t.pks[from],
		&typ,
		&t.addresses[from],
		&method,
		&payload,
	)
}

func encodeWalletTx(tb testing.TB, pk ed25519.PrivateKey, fields ...scale.Encodable) []byte {
	tb.Helper()

	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	for _, field := range fields {
		_, err := field.EncodeScale(encoder)
		require.NoError(tb, err)
	}
	hash := core.Hash(buf.Bytes())

	sig := ed25519.Sign(pk, hash[:])
	var sigfield core.Signature
	copy(sigfield[:], sig)
	_, err := sigfield.EncodeScale(encoder)
	require.NoError(tb, err)
	return buf.Bytes()
}

type testTx interface {
	gen(*tester) []byte
}

type spawnWallet struct {
	principal int
}

func (tx *spawnWallet) gen(t *tester) []byte {
	return t.selfSpawnWallet(tx.principal)
}

type spendWallet struct {
	from, to int
	amount   uint64
}

func (tx *spendWallet) gen(t *tester) []byte {
	return t.spendWallet(tx.from, tx.to, tx.amount)
}

type change interface {
	verify(tb testing.TB, prev, current *core.Account)
}

type same struct{}

func (ch same) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()
	require.Equal(tb, prev, current)
}

type spawned struct {
	template core.Address
	change
}

func (ch spawned) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()

	require.Nil(tb, prev.Template)
	require.Nil(tb, prev.State)

	require.Equal(tb, ch.template, *current.Template)
	require.NotNil(tb, current.State)

	prev.Template = current.Template
	prev.State = current.State
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type earned struct {
	amount int
	change
}

func (ch earned) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()
	require.Equal(tb, ch.amount, int(current.Balance-prev.Balance))

	prev.Balance = current.Balance
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type spent struct {
	amount int
	change change
}

func (ch spent) verify(tb testing.TB, prev, current *core.Account) {
	tb.Helper()
	require.Equal(tb, ch.amount, int(prev.Balance-current.Balance))

	prev.Balance = current.Balance
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type nonce struct {
	increased int
	next      change
}

func (ch nonce) verify(tb testing.TB, prev, current *core.Account) {
	require.Equal(tb, ch.increased, int(current.Nonce-prev.Nonce))
	if ch.next != nil {
		ch.next.verify(tb, prev, current)
	}
}

func TestWorkflow(t *testing.T) {
	const (
		funded  = 10  // number of funded accounts, included in genesis
		total   = 100 // total number of accounts
		balance = 1_000_000_000

		defaultGasPrice = 1
	)

	type layertc struct {
		txs      []testTx
		expected map[int]change
		skipped  []int
	}
	for _, tc := range []struct {
		desc   string
		layers []layertc
	}{
		{
			desc: "Sanity",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
					},
					expected: map[int]change{
						0: spawned{template: wallet.TemplateAddress},
						1: same{},
					},
				},
				{
					txs: []testTx{
						&spendWallet{0, 10, 100},
					},
					expected: map[int]change{
						0:  spent{amount: 100 + defaultGasPrice*wallet.TotalGasSpend},
						1:  same{},
						10: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "SpawnSpend",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
						&spendWallet{0, 10, 100},
					},
					expected: map[int]change{
						0: spawned{template: wallet.TemplateAddress,
							change: spent{amount: 100 + defaultGasPrice*(wallet.TotalGasSpend+wallet.TotalGasSpawn)}},
						10: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "MultipleSpends",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
					},
				},
				{
					txs: []testTx{
						&spendWallet{0, 10, 100},
						&spendWallet{0, 11, 100},
						&spendWallet{0, 12, 100},
					},
					expected: map[int]change{
						0:  spent{amount: 100*3 + defaultGasPrice*3*wallet.TotalGasSpend},
						10: earned{amount: 100},
						11: earned{amount: 100},
						12: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "SpendReceived",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
					},
				},
				{
					txs: []testTx{
						&spendWallet{0, 10, 1000},
						&spawnWallet{10},
						&spendWallet{10, 11, 100},
					},
					expected: map[int]change{
						0: spent{amount: 1000 + defaultGasPrice*wallet.TotalGasSpend},
						10: spawned{
							template: wallet.TemplateAddress,
							change:   earned{amount: 1000 - 100 - defaultGasPrice*(wallet.TotalGasSpawn+wallet.TotalGasSpend)},
						},
						11: earned{amount: 100},
					},
				},
				{
					txs: []testTx{
						&spendWallet{10, 11, 100},
						&spendWallet{10, 12, 100},
					},
					expected: map[int]change{
						10: spent{amount: 2*100 + 2*defaultGasPrice*wallet.TotalGasSpend},
						11: earned{amount: 100},
						12: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "StateChangedTransfer",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
						&spawnWallet{1},
					},
				},
				{
					txs: []testTx{
						&spendWallet{1, 0, 1000},
						&spendWallet{0, 10, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * wallet.TotalGasSpend,
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*wallet.TotalGasSpend},
						10: earned{amount: 1000},
					},
				},
				{
					txs: []testTx{
						&spendWallet{0, 10, 1000},
						&spendWallet{1, 0, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * wallet.TotalGasSpend,
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*wallet.TotalGasSpend},
						10: earned{amount: 1000},
					},
				},
			},
		},
		{
			desc: "SendToIself",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
					},
				},
				{

					txs: []testTx{
						&spendWallet{0, 0, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * wallet.TotalGasSpend,
							change: nonce{increased: 1},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			tt := newTester(t).
				addAccounts(funded).
				applyGenesisWithBalance(balance).
				addAccounts(total - funded)

			for i, layer := range tc.layers {
				var txs [][]byte
				for _, gen := range layer.txs {
					txs = append(txs, gen.gen(tt))
				}
				lid := types.NewLayerID(uint32(i + 1))
				skipped, err := tt.Apply(lid, txs)
				require.NoError(tt, err)
				if layer.skipped == nil {
					require.Empty(tt, skipped)
				} else {
					for i, pos := range layer.skipped {
						require.Equal(t, txs[pos], skipped[i])
					}
				}
				for account, changes := range layer.expected {
					prev, err := accounts.Get(tt.db, tt.addresses[account], lid.Sub(1))
					require.NoError(tt, err)
					current, err := accounts.Get(tt.db, tt.addresses[account], lid)
					require.NoError(tt, err)
					changes.verify(tt, &prev, &current)
				}
			}
		})
	}
}

func TestRandomTransfers(t *testing.T) {
	tt := newTester(t).withSeed(101).
		addAccounts(10).
		applyGenesis()

	skipped, err := tt.Apply(types.NewLayerID(1), tt.spawnWalletAll())
	require.NoError(tt, err)
	require.Empty(tt, skipped)
	for i := 0; i < 1000; i++ {
		lid := types.NewLayerID(2).Add(uint32(i))
		skipped, err := tt.Apply(lid, tt.randSendWalletN(20, 10))
		require.NoError(tt, err)
		require.Empty(tt, skipped)
	}
}

func TestValidation(t *testing.T) {
	tt := newTester(t).addAccounts(2).applyGenesis()
	req := tt.Validation(tt.selfSpawnWallet(0))
	header, err := req.Parse()
	require.NoError(t, err)
	require.Empty(t, header.Nonce)
	require.True(t, req.Verify())
}

func FuzzParse(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		tt := newTester(t).addAccounts(1).applyGenesis()
		req := tt.Validation(data)
		req.Parse()
	})
}

func BenchmarkValidation(b *testing.B) {
	tt := newTester(b).addAccounts(2).applyGenesis()
	skipped, err := tt.Apply(types.NewLayerID(1), [][]byte{tt.selfSpawnWallet(0)})
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	bench := func(b *testing.B, raw []byte) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			req := tt.Validation(raw)
			_, err := req.Parse()
			if err != nil {
				b.Fatal(err)
			}
			if !req.Verify() {
				b.Fatalf("expected Verify to return true")
			}
		}
	}

	b.Run("SpawnWallet", func(b *testing.B) {
		bench(b, tt.selfSpawnWallet(1))
	})

	b.Run("SpendWallet", func(b *testing.B) {
		bench(b, tt.spendWallet(0, 1, 10))
	})
}

func BenchmarkWallet(b *testing.B) {
	b.Run("Accounts100k/Txs100k", func(b *testing.B) {
		benchmarkWallet(b, 100_000, 100_000)
	})
	b.Run("Accounts100k/Txs1kk", func(b *testing.B) {
		benchmarkWallet(b, 100_000, 1_000_000)
	})
}

func benchmarkWallet(b *testing.B, accounts, n int) {
	tt := newTester(b).persistent().
		addAccounts(accounts).applyGenesis().withSeed(101)
	lid := types.NewLayerID(1)
	skipped, err := tt.Apply(types.NewLayerID(1), tt.spawnWalletAll())
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	var layers [][][]byte
	for i := 0; i < b.N; i++ {
		layers = append(layers, tt.randSendWalletN(n, 10))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for _, txs := range layers {
		lid = lid.Add(1)
		skipped, err := tt.Apply(lid, txs)
		if err != nil {
			b.Fatal(err)
		}
		if skipped != nil {
			b.Fatalf("skipped transactions %v", skipped)
		}
	}
}
