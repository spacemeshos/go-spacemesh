package vm

import (
	"bytes"
	"math/big"
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
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

const (
	testBaseReward = 1000
	testGasLimit   = 100_000_000
)

func testContext(lid types.LayerID) ApplyContext {
	return ApplyContext{
		Layer: lid,
	}
}

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

func (t *tester) withBaseReward(reward uint64) *tester {
	t.VM.cfg.BaseReward = reward
	return t
}

func (t *tester) withGasLimit(limit uint64) *tester {
	t.VM.cfg.GasLimit = limit
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
		t.addresses = append(t.addresses, core.ComputePrincipal(wallet.TemplateAddress, &args))
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

func (t *tester) spawnWalletAll() []types.RawTx {
	var rst []types.RawTx
	for i := 0; i < len(t.addresses); i++ {
		if t.nonces[i].Counter != 0 {
			continue
		}
		rst = append(rst, t.selfSpawnWallet(i))
	}
	return rst
}

func (t *tester) selfSpawnWallet(i int) types.RawTx {
	typ := scale.U8(0)
	method := scale.U8(0)
	payload := wallet.SpawnPayload{GasPrice: 1}
	copy(payload.Arguments.PublicKey[:], t.pubs[i])
	t.nextNonce(i)
	return types.NewRawTx(encodeWalletTx(t, t.pks[i],
		&typ,
		&t.addresses[i], &method, &wallet.TemplateAddress,
		&payload,
	))
}

func (t *tester) randSendWalletN(n int, amount uint64) []types.RawTx {
	rst := make([]types.RawTx, n)
	for i := range rst {
		rst[i] = t.randSpendWallet(amount)
	}
	return rst
}

func (t *tester) randSpendWallet(amount uint64) types.RawTx {
	return t.spendWallet(t.rng.Intn(len(t.addresses)), t.rng.Intn(len(t.addresses)), amount)
}

func (t *tester) withSeed(seed int64) *tester {
	t.rng = rand.New(rand.NewSource(seed))
	return t
}

func (t *tester) spendWallet(from, to int, amount uint64) types.RawTx {
	return t.spendWalletWithNonce(from, to, amount, t.nextNonce(from))
}

type reward struct {
	address int
	share   float64
}

func (t *tester) rewards(all ...reward) []types.AnyReward {
	var rst []types.AnyReward
	for _, rew := range all {
		rat := new(big.Rat).SetFloat64(rew.share)
		rst = append(rst, types.AnyReward{
			Coinbase: t.addresses[rew.address],
			Weight: types.RatNum{
				Num:   rat.Num().Uint64(),
				Denom: rat.Denom().Uint64(),
			},
		})
	}
	return rst
}

func (t *tester) spendWalletWithNonce(from, to int, amount uint64, nonce core.Nonce) types.RawTx {
	payload := wallet.SpendPayload{}
	payload.Arguments.Destination = t.addresses[to]
	payload.Arguments.Amount = amount
	payload.GasPrice = 1
	payload.Nonce = nonce

	typ := scale.U8(0)
	method := scale.U8(1)
	return types.NewRawTx(encodeWalletTx(t, t.pks[from],
		&typ,
		&t.addresses[from],
		&method,
		&payload,
	))
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

func encodeFields(tb testing.TB, fields ...scale.Encodable) types.RawTx {
	tb.Helper()

	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	for _, field := range fields {
		_, err := field.EncodeScale(encoder)
		require.NoError(tb, err)
	}
	return types.NewRawTx(buf.Bytes())
}

type testTx interface {
	gen(*tester) types.RawTx
}

type spawnWallet struct {
	principal int
}

func (tx *spawnWallet) gen(t *tester) types.RawTx {
	return t.selfSpawnWallet(tx.principal)
}

type spendWallet struct {
	from, to int
	amount   uint64
}

func (tx *spendWallet) gen(t *tester) types.RawTx {
	return t.spendWallet(tx.from, tx.to, tx.amount)
}

func (tx spendWallet) withNonce(nonce core.Nonce) *spendWalletNonce {
	return &spendWalletNonce{spendWallet: tx, nonce: nonce}
}

type corruptSig struct {
	testTx
}

func (cs corruptSig) gen(t *tester) types.RawTx {
	tx := cs.testTx.gen(t)
	last := tx.Raw[len(tx.Raw)-1]
	if last == 255 {
		last--
	} else {
		last++
	}
	tx.Raw[len(tx.Raw)-1] = last
	return tx
}

type spendWalletNonce struct {
	spendWallet
	nonce core.Nonce
}

func (tx *spendWalletNonce) gen(t *tester) types.RawTx {
	return t.spendWalletWithNonce(tx.from, tx.to, tx.amount, tx.nonce)
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

	require.NotNil(tb, current.Template, "account should be spawned")
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
		rewards  []reward
		expected map[int]change
		gasLimit uint64

		ineffective []int            // list with references to ineffective txs
		headers     map[int]struct{} // is vm expected to return the header
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
						0: spawned{
							template: wallet.TemplateAddress,
							change:   spent{amount: 100 + defaultGasPrice*(wallet.TotalGasSpend+wallet.TotalGasSpawn)},
						},
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
		{
			desc: "SpendNoSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&spendWallet{0, 10, 1},
					},
					ineffective: []int{0},
					expected: map[int]change{
						0:  same{},
						10: same{},
					},
				},
			},
		},
		{
			desc: "NoFundsForSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{11},
					},
					ineffective: []int{0},
					expected: map[int]change{
						11: same{},
					},
				},
			},
		},
		{
			desc: "NoFundsForSend",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
						&spendWallet{0, 11, wallet.TotalGasSpawn},
						&spawnWallet{11},
						&spendWallet{11, 12, 1},
					},
					ineffective: []int{3},
					expected: map[int]change{
						11: spawned{template: wallet.TemplateAddress},
						12: same{},
					},
				},
			},
		},
		{
			desc: "BlockGasLimit",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
						&spendWallet{0, 10, 100},
						&spendWallet{0, 11, 100},
						&spendWallet{0, 12, 100},
					},
					gasLimit:    wallet.TotalGasSpawn + wallet.TotalGasSpend,
					ineffective: []int{2, 3},
					expected: map[int]change{
						0:  spent{amount: 100 + wallet.TotalGasSpawn + wallet.TotalGasSpend},
						10: earned{amount: 100},
						11: same{},
						12: same{},
					},
				},
			},
		},
		{
			desc: "BlockGasLimitIsNotConsumedByInefective",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
						&spawnWallet{10},
						&spendWallet{0, 10, 100},
						&spendWallet{0, 11, 100},
					},
					gasLimit:    wallet.TotalGasSpawn + wallet.TotalGasSpend,
					ineffective: []int{1, 3},
					expected: map[int]change{
						0:  spent{amount: 100 + wallet.TotalGasSpawn + wallet.TotalGasSpend},
						10: earned{amount: 100},
						11: same{},
					},
				},
			},
		},
		{
			desc: "BadNonceOrder",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
						spendWallet{0, 11, 100}.withNonce(core.Nonce{Counter: 2}),
						spendWallet{0, 10, 100}.withNonce(core.Nonce{Counter: 1}),
					},
					ineffective: []int{1},
					headers: map[int]struct{}{
						1: {},
					},
					expected: map[int]change{
						0: spawned{
							template: wallet.TemplateAddress,
							change:   spent{amount: 100 + defaultGasPrice*(wallet.TotalGasSpawn+wallet.TotalGasSpend)},
						},
						10: earned{amount: 100},
						11: same{},
					},
				},
				{
					txs: []testTx{
						spendWallet{0, 10, 100}.withNonce(core.Nonce{Counter: 2}),
						spendWallet{0, 12, 100}.withNonce(core.Nonce{Counter: 3}),
					},
					expected: map[int]change{
						0:  spent{amount: 2*100 + 2*defaultGasPrice*wallet.TotalGasSpend},
						10: earned{amount: 100},
						12: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "SpendRewards",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
					},
					rewards: []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: earned{amount: testBaseReward + wallet.TotalGasSpawn},
					},
				},
				{
					txs: []testTx{
						&spawnWallet{10},
					},
					rewards: []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: spawned{template: wallet.TemplateAddress},
					},
				},
			},
		},
		{
			desc: "DistributeRewards",
			layers: []layertc{
				{
					rewards: []reward{{address: 10, share: 0.5}, {address: 11, share: 0.5}},
					expected: map[int]change{
						10: earned{amount: testBaseReward * 0.5},
						11: earned{amount: testBaseReward * 0.5},
					},
				},
				{
					txs: []testTx{
						&spawnWallet{0},
					},
					rewards: []reward{{address: 10, share: 0.5}, {address: 11, share: 0.5}},
					expected: map[int]change{
						10: earned{amount: (testBaseReward + wallet.TotalGasSpawn) * 0.5},
						11: earned{amount: (testBaseReward + wallet.TotalGasSpawn) * 0.5},
					},
				},
			},
		},
		{
			desc: "SkippedTransactionsNotRewarded",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
					},
				},
				{
					txs: []testTx{
						spendWallet{0, 10, 100}.withNonce(core.Nonce{Counter: 2}),
						spendWallet{0, 11, 100}.withNonce(core.Nonce{Counter: 3}),
					},
					ineffective: []int{0, 1},
					headers:     map[int]struct{}{0: {}, 1: {}},
					rewards:     []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: earned{amount: testBaseReward},
					},
				},
			},
		},
		{
			desc: "FailVerify",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnWallet{0},
					},
				},
				{
					txs: []testTx{
						corruptSig{&spendWallet{0, 10, 100}},
					},
					ineffective: []int{0},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			tt := newTester(t).
				addAccounts(funded).
				applyGenesisWithBalance(balance).
				addAccounts(total - funded).
				withBaseReward(testBaseReward)

			for i, layer := range tc.layers {
				var txs []types.RawTx
				for _, gen := range layer.txs {
					txs = append(txs, gen.gen(tt))
				}
				lid := types.NewLayerID(uint32(i + 1))
				ctx := testContext(lid)
				if layer.gasLimit > 0 {
					tt = tt.withGasLimit(layer.gasLimit)
				}
				ineffective, _, err := tt.Apply(ctx, notVerified(txs...), tt.rewards(layer.rewards...))
				require.NoError(tt, err)
				if layer.ineffective == nil {
					require.Empty(tt, ineffective)
				} else {
					require.Len(tt, ineffective, len(layer.ineffective))
					for i, pos := range layer.ineffective {
						require.Equal(t, txs[pos].ID, ineffective[i].ID)
						_, exist := layer.headers[pos]
						if exist {
							require.NotNil(t, ineffective[i].TxHeader)
						} else {
							require.Nil(t, ineffective[i].TxHeader)
						}
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

	skipped, _, err := tt.Apply(testContext(types.NewLayerID(1)),
		notVerified(tt.spawnWalletAll()...), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)
	for i := 0; i < 1000; i++ {
		lid := types.NewLayerID(2).Add(uint32(i))
		skipped, _, err := tt.Apply(testContext(lid),
			notVerified(tt.randSendWalletN(20, 10)...), nil)
		require.NoError(tt, err)
		require.Empty(tt, skipped)
	}
}

func TestValidation(t *testing.T) {
	tt := newTester(t).
		addAccounts(1).
		applyGenesis().
		addAccounts(1)
	skipped, _, err := tt.Apply(testContext(types.NewLayerID(1)),
		notVerified(tt.selfSpawnWallet(0)), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	zero := scale.U8(0)
	one := scale.U8(1)
	two := scale.U8(2)

	for _, tc := range []struct {
		desc   string
		tx     types.RawTx
		header *core.Header
		err    error
	}{
		{
			desc: "Spawn",
			tx:   tt.selfSpawnWallet(1),
			header: &core.Header{
				Principal: tt.addresses[1],
				Method:    0,
				Template:  wallet.TemplateAddress,
				GasPrice:  1,
				MaxGas:    wallet.TotalGasSpawn,
			},
		},
		{
			desc: "Spend",
			tx:   tt.spendWallet(0, 1, 100),
			header: &core.Header{
				Principal: tt.addresses[0],
				Method:    1,
				Template:  wallet.TemplateAddress,
				GasPrice:  1,
				Nonce:     core.Nonce{Counter: 1},
				MaxSpend:  100,
				MaxGas:    wallet.TotalGasSpend,
			},
		},
		{
			desc: "WrongVersion",
			tx:   encodeFields(tt, &one),
			err:  core.ErrMalformed,
		},
		{
			desc: "InvalidPrincipal",
			tx:   encodeFields(tt, &one, &one),
			err:  core.ErrMalformed,
		},
		{
			desc: "InvalidTemplate",
			tx:   encodeFields(tt, &zero, &tt.addresses[0], &zero, &one),
			err:  core.ErrMalformed,
		},
		{
			desc: "UnknownTemplate",
			tx:   encodeFields(tt, &zero, &tt.addresses[0], &zero, &tt.addresses[0]),
			err:  core.ErrMalformed,
		},
		{
			desc: "UnknownMethod",
			tx:   encodeFields(tt, &zero, &tt.addresses[0], &two),
			err:  core.ErrMalformed,
		},
		{
			desc: "NotSpawned",
			tx:   tt.spendWallet(1, 1, 100),
			err:  core.ErrNotSpawned,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := tt.Validation(tc.tx)
			header, err := req.Parse()
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.Equal(t, tc.header, header)
				require.True(t, req.Verify())
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		tt := newTester(t).addAccounts(1).applyGenesis()
		req := tt.Validation(types.NewRawTx(data))
		req.Parse()
	})
}

func BenchmarkValidation(b *testing.B) {
	tt := newTester(b).addAccounts(2).applyGenesis()
	skipped, _, err := tt.Apply(ApplyContext{Layer: types.NewLayerID(1)},
		notVerified(tt.selfSpawnWallet(0)), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	bench := func(b *testing.B, raw types.RawTx) {
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

func TestStateHashFromUpdatedAccounts(t *testing.T) {
	tt := newTester(t).addAccounts(10).applyGenesis()

	lid := types.NewLayerID(1)
	skipped, _, err := tt.Apply(testContext(lid), notVerified(
		tt.selfSpawnWallet(0),
		tt.selfSpawnWallet(1),
		tt.spendWallet(0, 2, 100),
		tt.spendWallet(1, 4, 100),
	), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	expected := types.Hash32{}
	hasher := hash.New()
	encoder := scale.NewEncoder(hasher)
	for _, pos := range []int{0, 1, 2, 4} {
		account, err := accounts.Get(tt.db, tt.addresses[pos], lid)
		require.NoError(t, err)
		account.EncodeScale(encoder)
	}
	hasher.Sum(expected[:0])

	statehash, err := layers.GetStateHash(tt.db, lid)
	require.NoError(t, err)
	require.Equal(t, expected, statehash)
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
	skipped, _, err := tt.Apply(ApplyContext{Layer: types.NewLayerID(1)},
		notVerified(tt.spawnWalletAll()...), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	var layers [][]types.Transaction
	for i := 0; i < b.N; i++ {
		raw := tt.randSendWalletN(n, 10)
		parsed := make([]types.Transaction, 0, len(raw))
		for _, tx := range raw {
			val := tt.Validation(tx)
			header, err := val.Parse()
			require.NoError(b, err)
			parsed = append(parsed, types.Transaction{
				RawTx:    tx,
				TxHeader: header,
			})
		}
		layers = append(layers, parsed)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for _, txs := range layers {
		lid = lid.Add(1)
		skipped, _, err := tt.Apply(testContext(lid), txs, nil)
		if err != nil {
			b.Fatal(err)
		}
		if skipped != nil {
			b.Fatalf("skipped transactions %v", skipped)
		}
	}
}

func notVerified(raw ...types.RawTx) []types.Transaction {
	var rst []types.Transaction
	for _, tx := range raw {
		rst = append(rst, types.Transaction{RawTx: tx})
	}
	return rst
}
