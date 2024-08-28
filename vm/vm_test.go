package vm

import (
	"bytes"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/economics/rewards"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	sdkwallet "github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

func testContext(lid types.LayerID) ApplyContext {
	return ApplyContext{
		Layer: lid,
	}
}

func newTester(tb testing.TB) *tester {
	return &tester{
		TB: tb,
		VM: New(statesql.InMemory(),
			WithLogger(zaptest.NewLogger(tb)),
			WithConfig(Config{GasLimit: math.MaxUint64}),
		),
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

type testAccount interface {
	getAddress() core.Address
	getTemplate() core.Address
	spend(to core.Address, amount uint64, nonce core.Nonce, opts ...sdk.Opt) []byte
	selfSpawn(nonce core.Nonce, opts ...sdk.Opt) []byte

	spawn(template core.Address, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte
	spawnArgs() scale.Encodable

	baseGas(uint8) int
	loadGas() int
	execGas(uint8) int
}

type singlesigAccount struct {
	pk      ed25519.PrivateKey
	address core.Address
}

func (a *singlesigAccount) getAddress() core.Address {
	return a.address
}

func (a *singlesigAccount) getTemplate() core.Address {
	return wallet.TemplateAddress
}

func (a *singlesigAccount) spend(to core.Address, amount uint64, nonce core.Nonce, opts ...sdk.Opt) []byte {
	return sdkwallet.Spend(a.pk, to, amount, nonce, opts...)
}

func (a *singlesigAccount) selfSpawn(nonce core.Nonce, opts ...sdk.Opt) []byte {
	return sdkwallet.SelfSpawn(a.pk, nonce, opts...)
}

func (a *singlesigAccount) spawn(
	template core.Address,
	args scale.Encodable,
	nonce core.Nonce,
	opts ...sdk.Opt,
) []byte {
	return sdkwallet.Spawn(a.pk, template, args, nonce, opts...)
}

func (a *singlesigAccount) spawnArgs() scale.Encodable {
	args := wallet.SpawnArguments{}
	copy(args.PublicKey[:], signing.Public(a.pk))
	return &args
}

func (a *singlesigAccount) baseGas(method uint8) int {
	return int(wallet.BaseGas(method))
}

func (a *singlesigAccount) loadGas() int {
	return int(wallet.LoadGas())
}

func (a *singlesigAccount) execGas(method uint8) int {
	return int(wallet.ExecGas(method))
}

type tester struct {
	testing.TB
	*VM

	rng *rand.Rand

	accounts []testAccount
	nonces   []core.Nonce
	balances []uint64
}

func (t *tester) persistent() *tester {
	db, err := statesql.Open("file:" + filepath.Join(t.TempDir(), "test.sql"))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, err)
	t.VM = New(db, WithLogger(zaptest.NewLogger(t)),
		WithConfig(Config{GasLimit: math.MaxUint64}))
	return t
}

func (t *tester) withGasLimit(limit uint64) *tester {
	t.VM.cfg.GasLimit = limit
	return t
}

func (t *tester) addAccount(account testAccount, balance uint64) {
	t.accounts = append(t.accounts, account)
	t.nonces = append(t.nonces, 0)
	t.balances = append(t.balances, balance)
}

func (t *tester) addSingleSig(n int) *tester {
	for i := 0; i < n; i++ {
		pub, pk, err := ed25519.GenerateKey(t.rng)
		require.NoError(t, err)
		t.addAccount(&singlesigAccount{pk: pk, address: sdkwallet.Address(pub)}, 1_000_000_000)
	}
	return t
}

func (t *tester) applyGenesis() *tester {
	return t.applyGenesisWithBalance()
}

func (t *tester) applyGenesisWithBalance() *tester {
	accounts := make([]core.Account, len(t.accounts))
	for i := range accounts {
		accounts[i] = core.Account{
			Address: t.accounts[i].getAddress(),
			Balance: t.balances[i],
		}
	}
	require.NoError(t, t.VM.ApplyGenesis(accounts))
	return t
}

func (t *tester) nextNonce(i int) core.Nonce {
	nonce := t.nonces[i]
	t.nonces[i]++
	return nonce
}

func (t *tester) spawnAll() []types.RawTx {
	var rst []types.RawTx
	for i := 0; i < len(t.accounts); i++ {
		if t.nonces[i] != 0 {
			continue
		}
		rst = append(rst, t.selfSpawn(i))
	}
	return rst
}

func (t *tester) selfSpawn(i int, opts ...sdk.Opt) types.RawTx {
	nonce := t.nextNonce(i)
	return types.NewRawTx(t.accounts[i].selfSpawn(nonce, opts...))
}

func (t *tester) spawn(i, j int, opts ...sdk.Opt) types.RawTx {
	nonce := t.nextNonce(i)
	return types.NewRawTx(t.accounts[i].spawn(t.accounts[j].getTemplate(), t.accounts[j].spawnArgs(), nonce, opts...))
}

func (t *tester) randSpendN(n int, amount uint64) []types.RawTx {
	rst := make([]types.RawTx, n)
	for i := range rst {
		rst[i] = t.randSpend(amount)
	}
	return rst
}

func (t *tester) randSpend(amount uint64) types.RawTx {
	return t.spend(t.rng.Intn(len(t.accounts)), t.rng.Intn(len(t.accounts)), amount)
}

func (t *tester) withSeed(seed int64) *tester {
	t.rng = rand.New(rand.NewSource(seed))
	return t
}

func (t *tester) spend(from, to int, amount uint64, opts ...sdk.Opt) types.RawTx {
	return t.spendWithNonce(from, to, amount, t.nextNonce(from), opts...)
}

func (t *tester) spendWithNonce(from, to int, amount uint64, nonce core.Nonce, opts ...sdk.Opt) types.RawTx {
	return types.NewRawTx(t.accounts[from].spend(t.accounts[to].getAddress(), amount, nonce, opts...))
}

type reward struct {
	address int
	share   float64
}

func (t *tester) rewards(all ...reward) []types.CoinbaseReward {
	var rst []types.CoinbaseReward
	for _, rew := range all {
		rat := new(big.Rat).SetFloat64(rew.share)
		address := t.accounts[rew.address].getAddress()
		rst = append(rst, types.CoinbaseReward{
			Coinbase: address,
			// smesherID doesn't matter but must be set. Derive it arbitrarily from the coinbase.
			SmesherID: types.BytesToNodeID(address.Bytes()),
			Weight: types.RatNum{
				Num:   rat.Num().Uint64(),
				Denom: rat.Denom().Uint64(),
			},
		})
	}
	return rst
}

func (t *tester) estimateSpawnGas(principal, target int) int {
	args := t.accounts[target].spawnArgs()
	tx := t.accounts[principal].spawn(t.accounts[target].getTemplate(), args, 0)
	gas := t.accounts[principal].baseGas(core.MethodSpawn) +
		t.accounts[target].execGas(core.MethodSpawn) +
		int(core.TxDataGas(len(tx)))
	if principal != target {
		gas += t.accounts[principal].loadGas()
	}
	return gas
}

func (t *tester) estimateSpendGas(principal, to, amount int, nonce core.Nonce) int {
	tx := t.accounts[principal].spend(t.accounts[to].getAddress(), uint64(amount), nonce)
	return t.accounts[principal].baseGas(core.MethodSpend) +
		t.accounts[principal].loadGas() +
		t.accounts[principal].execGas(core.MethodSpend) +
		int(core.TxDataGas(len(tx)))
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

type selfSpawnTx struct {
	principal int
}

func (tx *selfSpawnTx) gen(t *tester) types.RawTx {
	return t.selfSpawn(tx.principal)
}

type selfSpawnTxWithOpts struct {
	principal int
	opts      []sdk.Opt
}

func (tx *selfSpawnTxWithOpts) gen(t *tester) types.RawTx {
	return t.selfSpawn(tx.principal, tx.opts...)
}

type spawnTx struct {
	principal, target int
}

func (tx *spawnTx) gen(t *tester) types.RawTx {
	return t.spawn(tx.principal, tx.target)
}

type spawnTxWithOpts struct {
	principal, target int
	opts              []sdk.Opt
}

func (tx *spawnTxWithOpts) gen(t *tester) types.RawTx {
	return t.spawn(tx.principal, tx.target, tx.opts...)
}

type spendTx struct {
	from, to int
	amount   uint64
}

func (tx *spendTx) gen(t *tester) types.RawTx {
	return t.spend(tx.from, tx.to, tx.amount)
}

func (tx spendTx) withNonce(nonce core.Nonce) *spendNonce {
	return &spendNonce{spendTx: tx, nonce: nonce}
}

type spendTxWithOpts struct {
	from, to int
	amount   uint64
	opts     []sdk.Opt
}

func (tx *spendTxWithOpts) gen(t *tester) types.RawTx {
	return t.spend(tx.from, tx.to, tx.amount, tx.opts...)
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

type spendNonce struct {
	spendTx
	nonce core.Nonce
}

func (tx *spendNonce) gen(t *tester) types.RawTx {
	return t.spendWithNonce(tx.from, tx.to, tx.amount, tx.nonce)
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

	require.Nil(tb, prev.TemplateAddress)
	require.Nil(tb, prev.State)

	require.NotNil(tb, current.TemplateAddress, "account should be spawned")
	require.Equal(tb, ch.template, *current.TemplateAddress)
	require.NotNil(tb, current.State)

	prev.TemplateAddress = current.TemplateAddress
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
	change    change
}

func (ch nonce) verify(tb testing.TB, prev, current *core.Account) {
	require.Equal(tb, ch.increased, int(current.NextNonce-prev.NextNonce))
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

type templateTestCase struct {
	desc   string
	layers []layertc
}

type layertc struct {
	txs      []testTx
	rewards  []reward
	expected map[int]change
	gasLimit uint64

	ineffective []int            // list with references to ineffective txs
	failed      map[int]error    // map with references to failed transaction, with specified error
	headers     map[int]struct{} // is vm expected to return the header
}

func singleWalletTestCases(defaultGasPrice int, template core.Address, ref *tester) []templateTestCase {
	return []templateTestCase{
		{
			desc: "Sanity",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
					expected: map[int]change{
						0: spawned{template: template},
						1: same{},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 100},
					},
					expected: map[int]change{
						0:  spent{amount: 100 + defaultGasPrice*ref.estimateSpendGas(0, 10, 100, 1)},
						1:  same{},
						10: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "wrong id for self-spawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTxWithOpts{0, []sdk.Opt{sdk.WithGenesisID(types.Hash20{1})}},
					},
					ineffective: []int{0},
					expected: map[int]change{
						0: same{},
					},
				},
			},
		},
		{
			desc: "SpawnSpend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 10, 100},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{amount: 100 +
								defaultGasPrice*
									(ref.estimateSpawnGas(0, 0)+
										ref.estimateSpendGas(0, 10, 100, 1))},
						},
						10: earned{amount: 100},
					},
				},
			},
		},
		{
			desc: "wrong id for spend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTxWithOpts{0, 10, 100, []sdk.Opt{sdk.WithGenesisID(types.Hash20{1})}},
					},
					ineffective: []int{1},
					expected: map[int]change{
						0: spawned{
							template: template,
							change:   spent{amount: defaultGasPrice * ref.estimateSpawnGas(0, 0)},
						},
						10: same{},
					},
				},
			},
		},
		{
			desc: "MultipleSpends",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 100},
						&spendTx{0, 11, 100},
						&spendTx{0, 12, 100},
					},
					expected: map[int]change{
						0: spent{amount: 100*3 + defaultGasPrice*
							(ref.estimateSpendGas(0, 10, 100, 1)+
								ref.estimateSpendGas(0, 11, 100, 2)+
								ref.estimateSpendGas(0, 12, 100, 3))},
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
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 2_000_000},
						&selfSpawnTx{10},
						&spendTx{10, 11, 100},
					},
					expected: map[int]change{
						0: spent{amount: 2_000_000 + defaultGasPrice*
							ref.estimateSpendGas(0, 10, 200_000, 1)},
						10: spawned{
							template: template,
							change: earned{amount: 2_000_000 - 100 - defaultGasPrice*(ref.estimateSpawnGas(10, 10)+
								ref.estimateSpendGas(10, 11, 100, 1))},
						},
						11: earned{amount: 100},
					},
				},
				{
					txs: []testTx{
						&spendTx{10, 11, 100},
						&spendTx{10, 12, 100},
					},
					expected: map[int]change{
						10: spent{amount: 2*100 + defaultGasPrice*
							(ref.estimateSpendGas(10, 11, 100, 2)+
								ref.estimateSpendGas(10, 12, 100, 3))},
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
						&selfSpawnTx{0},
						&selfSpawnTx{1},
					},
				},
				{
					txs: []testTx{
						&spendTx{1, 0, 1000},
						&spendTx{0, 10, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 10, 1000, 1),
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*ref.estimateSpendGas(1, 0, 1000, 1)},
						10: earned{amount: 1000},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 1000},
						&spendTx{1, 0, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 10, 1000, 1),
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*ref.estimateSpendGas(1, 0, 1000, 1)},
						10: earned{amount: 1000},
					},
				},
			},
		},
		{
			desc: "SendToSelf",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 0, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 0, 1000, 1),
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
						&spendTx{0, 10, 1},
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
						&selfSpawnTx{11},
					},
					ineffective: []int{0},
					expected: map[int]change{
						11: same{},
					},
				},
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11, 11)) - 1},
						&selfSpawnTx{11},
					},
					failed: map[int]error{2: core.ErrOutOfGas},
					expected: map[int]change{
						// incresed by two because previous was ineffective
						// but internal nonce in tester was incremented
						11: nonce{increased: 2},
					},
				},
			},
		},
		{
			desc: "NoFundsForSpend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(0, 0) * defaultGasPrice)},
						&selfSpawnTx{11},
						&spendTx{11, 12, 1},
					},
					ineffective: []int{3},
					expected: map[int]change{
						11: spawned{template: template, change: nonce{increased: 1}},
						12: same{},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 11, uint64((ref.estimateSpendGas(11, 12, 1, 1) - 1) * defaultGasPrice)},
						// send enough funds to cover spawn, but no spend
						&spendTx{11, 12, 1},
					},
					failed: map[int]error{1: core.ErrOutOfGas},
					expected: map[int]change{
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
						&selfSpawnTx{0},
						&spendTx{0, 10, 100},
						&spendTx{0, 11, 100},
						&spendTx{0, 12, 100},
					},
					gasLimit: uint64(ref.estimateSpawnGas(0, 0) +
						ref.estimateSpendGas(0, 10, 100, 1)),
					ineffective: []int{2, 3},
					expected: map[int]change{
						0:  spent{amount: 100 + ref.estimateSpawnGas(0, 0) + ref.estimateSpendGas(0, 10, 100, 1)},
						10: earned{amount: 100},
						11: same{},
						12: same{},
					},
				},
			},
		},
		{
			desc: "BlockGasLimitIsNotConsumedByIneffective",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 10, 80_000}, // send enough to cover intrinsic cost but not whole transaction
						&selfSpawnTx{10},
						&spendTx{0, 11, 100},
					},
					gasLimit: uint64(ref.estimateSpawnGas(0, 0) +
						ref.estimateSpendGas(0, 10, 80_000, 1) +
						ref.estimateSpawnGas(10, 10)),
					failed:      map[int]error{2: core.ErrOutOfGas},
					ineffective: []int{3},
					expected: map[int]change{
						0: spent{amount: 80_000 +
							ref.estimateSpawnGas(0, 0) +
							ref.estimateSpendGas(0, 10, 80_000, 1)},
						10: nonce{increased: 1},
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
						&selfSpawnTx{0},
						spendTx{0, 11, 100}.withNonce(2),
						spendTx{0, 10, 100}.withNonce(1),
					},
					ineffective: []int{2},
					headers: map[int]struct{}{
						2: {},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{
								amount: 100 + defaultGasPrice*(ref.estimateSpawnGas(0, 0)+
									ref.estimateSpendGas(0, 11, 100, 2)),
							},
						},
						10: same{},
						11: earned{amount: 100},
					},
				},
				{
					txs: []testTx{
						spendTx{0, 10, 100}.withNonce(3),
						spendTx{0, 12, 100}.withNonce(6),
					},
					expected: map[int]change{
						0: spent{amount: 2*100 + defaultGasPrice*(ref.estimateSpendGas(0, 10, 100, 3)+
							ref.estimateSpendGas(0, 10, 100, 6))},
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
						&selfSpawnTx{0},
					},
					rewards: []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: earned{amount: int(rewards.TotalSubsidyAtLayer(0)) + ref.estimateSpawnGas(0, 0)},
					},
				},
				{
					txs: []testTx{
						&selfSpawnTx{10},
					},
					rewards: []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: spawned{template: template},
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
						10: earned{amount: int(rewards.TotalSubsidyAtLayer(0)) / 2},
						11: earned{amount: int(rewards.TotalSubsidyAtLayer(0)) / 2},
					},
				},
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
					rewards: []reward{{address: 10, share: 0.5}, {address: 11, share: 0.5}},
					expected: map[int]change{
						10: earned{amount: (int(rewards.TotalSubsidyAtLayer(1)) + ref.estimateSpawnGas(10, 10)) / 2},
						11: earned{amount: (int(rewards.TotalSubsidyAtLayer(1)) + ref.estimateSpawnGas(11, 11)) / 2},
					},
				},
			},
		},
		{
			desc: "SkippedTransactionsNotRewarded",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						spendTx{0, 10, 100}.withNonce(5),
					},
				},
				{
					txs: []testTx{
						spendTx{0, 10, 100}.withNonce(2),
						spendTx{0, 11, 100}.withNonce(3),
					},
					ineffective: []int{0, 1},
					headers:     map[int]struct{}{0: {}, 1: {}},
					rewards:     []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: earned{amount: int(rewards.TotalSubsidyAtLayer(1))},
					},
				},
			},
		},
		{
			desc: "FailVerify",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
					},
				},
				{
					txs: []testTx{
						corruptSig{&spendTx{0, 10, 100}},
					},
					ineffective: []int{0},
				},
			},
		},
		{
			desc: "RetrySpend",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11, 11) + ref.estimateSpendGas(11, 12, 1_000, 1))},
						&selfSpawnTx{11},
						&spendTx{11, 12, 1_000},
					},
					failed: map[int]error{3: core.ErrNoBalance},
					expected: map[int]change{
						11: spawned{template: template, change: nonce{increased: 2}},
						12: same{},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 11, 200_000},
						&spendTx{11, 12, 1_000},
					},
					expected: map[int]change{
						0: spent{
							amount: ref.estimateSpendGas(0, 11, 200_000, 2) + 200_000,
							change: nonce{increased: 1},
						},
						11: nonce{increased: 1},
						12: earned{amount: 1_000},
					},
				},
			},
		},
		{
			desc: "RetrySelfSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11, 11)) - 1},
						&selfSpawnTx{11},
					},
					failed: map[int]error{2: core.ErrOutOfGas},
					expected: map[int]change{
						0: spent{amount: ref.estimateSpawnGas(11, 11) - 1 +
							ref.estimateSpawnGas(0, 0) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11, 11)-1, 1)},
						11: nonce{increased: 1},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11, 11))},
						&selfSpawnTx{11},
					},
					expected: map[int]change{
						0: spent{amount: ref.estimateSpawnGas(11, 11) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11, 11), 2)},
						11: spawned{template: template, change: nonce{increased: 1}},
					},
				},
			},
		},
		{
			desc: "SelfSpawnFailed",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&selfSpawnTx{0},
					},
					failed: map[int]error{1: core.ErrSpawned},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: nonce{
								increased: 2,
								change:    spent{amount: 2 * ref.estimateSpawnGas(0, 0)},
							},
						},
					},
				},
			},
		},
		{
			desc: "FailedFeesAndGas",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						// gas will be higher than fixed, but less than max gas
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11, 11)) - 1},
						// it will cause this transaction to be failed
						&selfSpawnTx{11},
					},
					gasLimit: uint64(ref.estimateSpawnGas(0, 0) +
						ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11, 11)-1, 1) +
						ref.estimateSpawnGas(11, 11)),
					failed:  map[int]error{2: core.ErrOutOfGas},
					rewards: []reward{{address: 20, share: 1}},
					expected: map[int]change{
						0: spent{amount: ref.estimateSpawnGas(0, 0) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11, 11)-1, 1) +
							ref.estimateSpawnGas(11, 11) - 1},
						11: nonce{increased: 1},
						// fees from every transaction (including failed) + testBaseReward
						20: earned{amount: ref.estimateSpawnGas(0, 0) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11, 11)-1, 1) +
							ref.estimateSpawnGas(11, 11) - 1 +
							int(rewards.TotalSubsidyAtLayer(0))},
					},
				},
			},
		},
		{
			desc: "Spawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTx{0, 11},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{
								amount: ref.estimateSpawnGas(0, 0) + ref.estimateSpawnGas(0, 11),
								change: nonce{increased: 2},
							},
						},
						11: spawned{template: template},
					},
				},
			},
		},
		{
			desc: "WrongIdInSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTxWithOpts{0, 11, []sdk.Opt{sdk.WithGenesisID(types.Hash20{1})}},
					},
					ineffective: []int{1},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{
								amount: ref.estimateSpawnGas(0, 0),
								change: nonce{increased: 1},
							},
						},
						11: same{},
					},
				},
			},
		},
		{
			desc: "SpendFromSpawned",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spawnTx{0, 11},
						&spendTx{0, 11, 200 + uint64(ref.estimateSpendGas(11, 12, 200, 0))},
					},
					expected: map[int]change{
						11: spawned{template: template},
					},
				},
				{
					txs: []testTx{
						&spendTx{11, 12, 200},
					},
					expected: map[int]change{
						11: spent{amount: 200 +
							ref.estimateSpendGas(11, 12, 200, 0)},
						12: earned{amount: 200},
					},
				},
			},
		},
		{
			desc: "FailedSpawn",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTx{0},
						&spendTx{0, 11, uint64(2*ref.estimateSpawnGas(11, 11) - 1)},
						&selfSpawnTx{11},
						&spawnTx{11, 12},
					},
					expected: map[int]change{
						0:  spawned{template: template, change: nonce{increased: 2}},
						11: spawned{template: template, change: nonce{increased: 2}},
						12: same{},
					},
					failed: map[int]error{
						3: core.ErrOutOfGas,
					},
				},
				{
					txs: []testTx{
						&spawnTx{0, 12},
					},
					expected: map[int]change{
						12: spawned{template: template},
					},
				},
			},
		},
		{
			desc: "NotSpawned",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnTx{0, 11},
					},
					expected: map[int]change{
						0:  same{},
						11: same{},
					},
					ineffective: []int{0},
				},
			},
		},
		{
			desc: "IneffectiveZeroGasPrice",
			layers: []layertc{
				{
					txs: []testTx{
						&selfSpawnTxWithOpts{0, []sdk.Opt{sdk.WithGasPrice(0)}},
					},
					ineffective: []int{0},
					expected: map[int]change{
						0: same{},
					},
				},
			},
		},
	}
}

func runTestCases(t *testing.T, tcs []templateTestCase, genTester func(t *testing.T) *tester) {
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			tt := genTester(t)
			next := types.GetEffectiveGenesis()
			for i, layer := range tc.layers {
				var txs []types.RawTx
				for _, gen := range layer.txs {
					txs = append(txs, gen.gen(tt))
				}
				lid := next
				next = next.Add(1)
				ctx := testContext(lid)
				if layer.gasLimit > 0 {
					tt = tt.withGasLimit(layer.gasLimit)
				}
				ineffective, results, err := tt.Apply(ctx, notVerified(txs...), tt.rewards(layer.rewards...))
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
				for i, rst := range results {
					expected, exists := layer.failed[i]
					if !exists {
						require.Equal(
							t,
							types.TransactionSuccess.String(),
							rst.Status.String(),
							"layer=%s txnum=%d",
							lid,
							i,
						)
					} else {
						require.Equal(t,
							types.TransactionFailure.String(),
							rst.Status.String(),
							"layer=%s txnum=%d",
							lid,
							i)
						require.Equal(t, expected.Error(), rst.Message)
					}
				}
				for account, changes := range layer.expected {
					prev, err := accounts.Get(tt.db, tt.accounts[account].getAddress(), lid.Sub(1))
					require.NoError(tt, err)
					current, err := accounts.Get(tt.db, tt.accounts[account].getAddress(), lid)
					require.NoError(tt, err)
					tt.Logf("verifying account index=%d in layer index=%d", account, i)
					changes.verify(tt, &prev, &current)
				}
			}
		})
	}
}

func testWallet(t *testing.T, defaultGasPrice int, template core.Address, genTester func(t *testing.T) *tester) {
	t.Parallel()
	runTestCases(t,
		singleWalletTestCases(defaultGasPrice, template, genTester(t)),
		genTester,
	)
}

func TestWallets(t *testing.T) {
	t.Parallel()
	const (
		funded = 10  // number of funded accounts, included in genesis
		total  = 100 // total number of accounts

		defaultGasPrice = 1
	)
	t.Run("SingleSig", func(t *testing.T) {
		testWallet(t, defaultGasPrice, wallet.TemplateAddress, func(t *testing.T) *tester {
			return newTester(t).
				addSingleSig(funded).
				applyGenesisWithBalance().
				addSingleSig(total - funded)
		})
	})
}

func testValidation(t *testing.T, tt *tester, template core.Address) {
	t.Parallel()
	skipped, _, err := tt.Apply(testContext(types.GetEffectiveGenesis()),
		notVerified(tt.selfSpawn(0)), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	firstAddress := tt.accounts[0].getAddress()
	zero := scale.U8(0)
	one := scale.U8(1)
	two := scale.U8(2)

	for _, tc := range []struct {
		desc     string
		tx       types.RawTx
		header   *core.Header
		err      error
		verified bool
	}{
		{
			desc: "Spawn",
			tx:   tt.selfSpawn(1),
			header: &core.Header{
				Principal:       tt.accounts[1].getAddress(),
				Method:          core.MethodSpawn,
				TemplateAddress: template,
				GasPrice:        1,
				MaxGas:          uint64(tt.estimateSpawnGas(1, 1)),
			},
			verified: true,
		},
		{
			desc: "SpawnGenesisIdMismatch",
			tx:   tt.selfSpawn(1, sdk.WithGenesisID(types.Hash20{1})),
		},
		{
			desc: "Spend",
			tx:   tt.spend(0, 1, 100),
			header: &core.Header{
				Principal:       tt.accounts[0].getAddress(),
				Method:          core.MethodSpend,
				TemplateAddress: template,
				GasPrice:        1,
				Nonce:           1,
				MaxSpend:        100,
				MaxGas:          uint64(tt.estimateSpendGas(0, 1, 100, 1)),
			},
			verified: true,
		},
		{
			desc: "SpendGenesisIdMismatch",
			tx:   tt.spend(0, 1, 100, sdk.WithGenesisID(types.Hash20{1})),
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
			tx:   encodeFields(tt, &zero, &firstAddress, &zero, &one),
			err:  core.ErrMalformed,
		},
		{
			desc: "UnknownTemplate",
			tx:   encodeFields(tt, &zero, &firstAddress, &zero, &firstAddress),
			err:  core.ErrMalformed,
		},
		{
			desc: "UnknownMethod",
			tx:   encodeFields(tt, &zero, &firstAddress, &two),
			err:  core.ErrMalformed,
		},
		{
			desc: "NotSpawned",
			tx:   tt.spend(1, 1, 100),
			err:  core.ErrNotSpawned,
		},
		{
			desc: "SpawnNotSpawned",
			tx:   tt.spawn(1, 0),
			err:  core.ErrNotSpawned,
		},
		{
			desc: "OverflowsLimit",
			tx:   types.NewRawTx(make([]byte, core.TxSizeLimit+1)),
			err:  core.ErrTxLimit,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := tt.Validation(tc.tx)
			header, err := req.Parse()
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.Equal(t, tc.verified, req.Verify())
				if tc.verified {
					require.Equal(t, tc.header, header)
				}
			}
		})
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()
	t.Run("SingleSig", func(t *testing.T) {
		tt := newTester(t).
			addSingleSig(1).
			applyGenesis().
			addSingleSig(1)
		testValidation(t, tt, wallet.TemplateAddress)
	})
}

func BenchmarkTransactions(b *testing.B) {
	bench := func(b *testing.B, tt *tester, txs []types.Transaction) {
		lid := types.GetEffectiveGenesis().Add(2)
		for i := 0; i < b.N; i++ {
			b.StartTimer()
			ineffective, txs, err := tt.Apply(ApplyContext{Layer: lid}, txs, nil)
			b.StopTimer()
			require.NoError(b, err)
			require.Empty(b, ineffective)
			for _, tx := range txs {
				require.Equal(b, types.TransactionSuccess, tx.Status)
			}
			require.NoError(b, tt.Revert(lid.Sub(1)))
		}
	}
	const n = 10
	b.Logf("n=%d", n)
	// benchmarks below will have overhead beside the transaction itself.
	// they are useful mainly to collect execution profiles and make estimations based on them.
	b.Run("singlesig/selfspawn", func(b *testing.B) {
		tt := newTester(b).persistent().addSingleSig(n).applyGenesis()
		txs := make([]types.Transaction, n)
		for i := range txs {
			tx := &selfSpawnTx{principal: i}
			txs[i] = types.Transaction{RawTx: tx.gen(tt)}
		}
		bench(b, tt, txs)
	})
	b.Run("singlesig/spawn", func(b *testing.B) {
		tt := newTester(b).persistent().addSingleSig(n).applyGenesis()
		ineffective, _, err := tt.Apply(
			ApplyContext{Layer: types.GetEffectiveGenesis().Add(1)},
			notVerified(tt.spawnAll()...),
			nil,
		)
		tt = tt.addSingleSig(n)

		require.NoError(b, err)
		require.Empty(b, ineffective)
		txs := make([]types.Transaction, n)
		for i := range txs {
			tx := &spawnTx{principal: i, target: i + n}
			txs[i] = types.Transaction{RawTx: tx.gen(tt)}
		}
		bench(b, tt, txs)
	})
	b.Run("singlesig/spend", func(b *testing.B) {
		tt := newTester(b).persistent().addSingleSig(n).applyGenesis()
		ineffective, _, err := tt.Apply(
			ApplyContext{Layer: types.GetEffectiveGenesis().Add(1)},
			notVerified(tt.spawnAll()...),
			nil,
		)
		tt = tt.addSingleSig(n)

		require.NoError(b, err)
		require.Empty(b, ineffective)
		txs := make([]types.Transaction, n)
		for i := range txs {
			tx := &spendTx{from: i, to: i + n, amount: 10}
			txs[i] = types.Transaction{RawTx: tx.gen(tt)}
		}
		bench(b, tt, txs)
	})
}

func BenchmarkValidation(b *testing.B) {
	tt := newTester(b).addSingleSig(2).applyGenesis()
	skipped, _, err := tt.Apply(ApplyContext{Layer: types.LayerID(3)},
		notVerified(tt.selfSpawn(0)), nil)
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
		bench(b, tt.selfSpawn(1))
	})

	b.Run("SpendWallet", func(b *testing.B) {
		bench(b, tt.spend(0, 1, 10))
	})
}

func TestBeforeEffectiveGenesis(t *testing.T) {
	// sanity check that layers before effective genesis are not pushed to vm
	tt := newTester(t)
	_, _, err := tt.Apply(ApplyContext{Layer: types.GetEffectiveGenesis().Sub(1)}, nil, nil)
	require.ErrorIs(t, err, core.ErrInternal)
}

func TestStateHashFromUpdatedAccounts(t *testing.T) {
	tt := newTester(t).addSingleSig(10).applyGenesis()

	root, err := tt.GetStateRoot()
	require.NoError(t, err)
	require.Equal(t, types.Hash32{}, root)

	lid := types.GetEffectiveGenesis()
	skipped, _, err := tt.Apply(testContext(lid), notVerified(
		tt.selfSpawn(0),
		tt.selfSpawn(1),
		tt.spend(0, 2, 100),
		tt.spend(1, 4, 100),
	), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	expected := types.Hash32{}
	hasher := hash.New()
	encoder := scale.NewEncoder(hasher)
	for _, pos := range []int{0, 1, 2, 4} {
		account, err := accounts.Get(tt.db, tt.accounts[pos].getAddress(), lid)
		require.NoError(t, err)
		account.EncodeScale(encoder)
	}
	hasher.Sum(expected[:0])

	statehash, err := layers.GetStateHash(tt.db, lid)
	require.NoError(t, err)
	require.Equal(t, expected, statehash)

	root, err = tt.GetStateRoot()
	require.NoError(t, err)
	require.Equal(t, expected, root)
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
		addSingleSig(accounts).applyGenesis().withSeed(101)
	lid := types.LayerID(3)
	skipped, _, err := tt.Apply(ApplyContext{Layer: lid},
		notVerified(tt.spawnAll()...), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	var layers [][]types.Transaction
	for i := 0; i < b.N; i++ {
		raw := tt.randSpendN(n, 10)
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

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(2)
	os.Exit(m.Run())
}
