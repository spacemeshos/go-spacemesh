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
	sdkmultisig "github.com/spacemeshos/go-spacemesh/genvm/sdk/multisig"
	sdkwallet "github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
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

type testAccount interface {
	getAddress() core.Address
	spend(to core.Address, amount uint64, nonce core.Nonce) []byte
	selfSpawn(nonce core.Nonce) []byte

	// fixed gas for spawn and spend
	spawnGas() int
	spendGas() int
}

type singlesigAccount struct {
	pk      ed25519.PrivateKey
	address core.Address
}

func (a *singlesigAccount) getAddress() core.Address {
	return a.address
}

func (a *singlesigAccount) spend(to core.Address, amount uint64, nonce core.Nonce) []byte {
	return sdkwallet.Spend(signing.PrivateKey(a.pk), to, amount, nonce)
}

func (a *singlesigAccount) selfSpawn(nonce core.Nonce) []byte {
	return sdkwallet.SelfSpawn(signing.PrivateKey(a.pk), nonce)
}

func (a *singlesigAccount) spendGas() int {
	return wallet.TotalGasSpend
}

func (a *singlesigAccount) spawnGas() int {
	return wallet.TotalGasSpawn
}

type multisigAccount struct {
	k        int
	pks      []ed25519.PrivateKey
	address  core.Address
	template core.Address
}

func (a *multisigAccount) getAddress() core.Address {
	return a.address
}

func (a *multisigAccount) spend(to core.Address, amount uint64, nonce core.Nonce) []byte {
	agg := sdkmultisig.Spend(0, a.pks[0], a.address, to, amount, nonce)
	for i := 1; i < a.k; i++ {
		part := sdkmultisig.Spend(uint8(i), a.pks[i], a.address, to, amount, nonce)
		agg.Add(*part.Part(uint8(i)))
	}
	return agg.Raw()
}

func (a *multisigAccount) selfSpawn(nonce core.Nonce) []byte {
	var pubs []ed25519.PublicKey
	for _, pk := range a.pks {
		pubs = append(pubs, ed25519.PublicKey(signing.Public(signing.PrivateKey(pk))))
	}
	var agg *sdkmultisig.Aggregator
	for i := 0; i < a.k; i++ {
		part := sdkmultisig.SelfSpawn(uint8(i), a.pks[i], a.template, pubs, nonce)
		if agg == nil {
			agg = part
		} else {
			agg.Add(*part.Part(uint8(i)))
		}
	}
	return agg.Raw()
}

func (a *multisigAccount) spendGas() int {
	switch a.template {
	case multisig.TemplateAddress1:
		return multisig.TotalGasSpend1
	case multisig.TemplateAddress2:
		return multisig.TotalGasSpend2
	case multisig.TemplateAddress3:
		return multisig.TotalGasSpend3
	}
	panic("unknown template")
}

func (a *multisigAccount) spawnGas() int {
	switch a.template {
	case multisig.TemplateAddress1:
		return multisig.TotalGasSpawn1
	case multisig.TemplateAddress2:
		return multisig.TotalGasSpawn2
	case multisig.TemplateAddress3:
		return multisig.TotalGasSpawn3
	}
	panic("unknown template")
}

type tester struct {
	testing.TB
	*VM

	rng *rand.Rand

	accounts []testAccount
	nonces   []core.Nonce
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

func (t *tester) addSingleSig(n int) *tester {
	for i := 0; i < n; i++ {
		pub, pk, err := ed25519.GenerateKey(t.rng)
		require.NoError(t, err)
		t.accounts = append(t.accounts, &singlesigAccount{pk: pk, address: sdkwallet.Address(pub)})
		t.nonces = append(t.nonces, core.Nonce{})
	}
	return t
}

func (t *tester) addMultisig(total, k, n int, template core.Address) *tester {
	for i := 0; i < total; i++ {
		pks := []ed25519.PrivateKey{}
		pubs := [][]byte{}
		for j := 0; j < n; j++ {
			pub, pk, err := ed25519.GenerateKey(t.rng)
			require.NoError(t, err)
			pks = append(pks, pk)
			pubs = append(pubs, pub)
		}
		t.accounts = append(t.accounts, &multisigAccount{
			k:        k,
			pks:      pks,
			address:  sdkmultisig.Address(template, pubs...),
			template: template,
		})
		t.nonces = append(t.nonces, core.Nonce{})
	}
	return t
}

func (t *tester) applyGenesis() *tester {
	return t.applyGenesisWithBalance(1_000_000_000_000)
}

func (t *tester) applyGenesisWithBalance(amount uint64) *tester {
	accounts := make([]core.Account, len(t.accounts))
	for i := range accounts {
		accounts[i] = core.Account{
			Address: t.accounts[i].getAddress(),
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

func (t *tester) spawnAll() []types.RawTx {
	var rst []types.RawTx
	for i := 0; i < len(t.accounts); i++ {
		if t.nonces[i].Counter != 0 {
			continue
		}
		rst = append(rst, t.selfSpawn(i))
	}
	return rst
}

func (t *tester) selfSpawn(i int) types.RawTx {
	nonce := t.nextNonce(i)
	return types.NewRawTx(t.accounts[i].selfSpawn(nonce))
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

func (t *tester) spend(from, to int, amount uint64) types.RawTx {
	return t.spendWithNonce(from, to, amount, t.nextNonce(from))
}

func (t *tester) spendWithNonce(from, to int, amount uint64, nonce core.Nonce) types.RawTx {
	return types.NewRawTx(t.accounts[from].spend(t.accounts[to].getAddress(), amount, nonce))
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
			Coinbase: t.accounts[rew.address].getAddress(),
			Weight: types.RatNum{
				Num:   rat.Num().Uint64(),
				Denom: rat.Denom().Uint64(),
			},
		})
	}
	return rst
}

func (t *tester) estimateSpawnGas(principal int) int {
	return t.accounts[principal].spawnGas() +
		len(t.accounts[principal].selfSpawn(core.Nonce{}))*int(t.VM.cfg.StorageCostFactor)
}

func (t *tester) estimateSpendGas(principal, to, amount int, nonce core.Nonce) int {
	return t.accounts[principal].spendGas() + len(t.accounts[principal].spend(t.accounts[to].getAddress(), uint64(amount), nonce))*int(t.VM.cfg.StorageCostFactor)
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

type spawnTx struct {
	principal int
}

func (tx *spawnTx) gen(t *tester) types.RawTx {
	return t.selfSpawn(tx.principal)
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
	change    change
}

func (ch nonce) verify(tb testing.TB, prev, current *core.Account) {
	require.Equal(tb, ch.increased, int(current.NextNonce-prev.NextNonce))
	if ch.change != nil {
		ch.change.verify(tb, prev, current)
	}
}

func testWallet(t *testing.T, template core.Address, defaultGasPrice int, genTester func(t *testing.T) *tester) {
	// consumed gas is computed from two variables:
	// - fixed gas
	// - storage cost
	// fixed gas depends on the template and type of the method call
	// storage cost depends on the transaction size
	//
	// estimating the latter in advance is problematic, since we are using
	// variable sized fields (mostly integers) in encoding.
	// the only robust approach that i came up with is to recompute raw
	// transaction based on expectations before the test itself
	ref := genTester(t)

	type layertc struct {
		txs      []testTx
		rewards  []reward
		expected map[int]change
		gasLimit uint64

		ineffective []int            // list with references to ineffective txs
		failed      map[int]error    // map with references to failed transaction, with specified error
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
						&spawnTx{0},
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
						0:  spent{amount: 100 + defaultGasPrice*ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 1})},
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
						&spawnTx{0},
						&spendTx{0, 10, 100},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: spent{amount: 100 +
								defaultGasPrice*
									(ref.estimateSpawnGas(0)+
										ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 1}))},
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
						&spawnTx{0},
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
							(ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 1})+
								ref.estimateSpendGas(0, 11, 100, core.Nonce{Counter: 2})+
								ref.estimateSpendGas(0, 12, 100, core.Nonce{Counter: 3}))},
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
						&spawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 10, 10_000},
						&spawnTx{10},
						&spendTx{10, 11, 100},
					},
					expected: map[int]change{
						0: spent{amount: 10000 + defaultGasPrice*
							ref.estimateSpendGas(0, 10, 10_000, core.Nonce{Counter: 1})},
						10: spawned{
							template: template,
							change: earned{amount: 10000 - 100 - defaultGasPrice*(ref.estimateSpawnGas(10)+
								ref.estimateSpendGas(10, 11, 100, core.Nonce{Counter: 1}))},
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
							(ref.estimateSpendGas(10, 11, 100, core.Nonce{Counter: 2})+
								ref.estimateSpendGas(10, 12, 100, core.Nonce{Counter: 3}))},
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
						&spawnTx{0},
						&spawnTx{1},
					},
				},
				{
					txs: []testTx{
						&spendTx{1, 0, 1000},
						&spendTx{0, 10, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 10, 1000, core.Nonce{Counter: 1}),
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*ref.estimateSpendGas(1, 0, 1000, core.Nonce{Counter: 1})},
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
							amount: defaultGasPrice * ref.estimateSpendGas(0, 10, 1000, core.Nonce{Counter: 1}),
							change: nonce{increased: 1},
						},
						1:  spent{amount: 1000 + defaultGasPrice*ref.estimateSpendGas(1, 0, 1000, core.Nonce{Counter: 1})},
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
						&spawnTx{0},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 0, 1000},
					},
					expected: map[int]change{
						0: spent{
							amount: defaultGasPrice * ref.estimateSpendGas(0, 0, 1000, core.Nonce{Counter: 1}),
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
						&spawnTx{11},
					},
					ineffective: []int{0},
					expected: map[int]change{
						11: same{},
					},
				},
				{
					txs: []testTx{
						&spawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11)) - 1},
						&spawnTx{11},
					},
					failed: map[int]error{2: core.ErrNoBalance},
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
						&spawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(0) * defaultGasPrice)},
						&spawnTx{11},
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
						&spendTx{0, 11, uint64((ref.estimateSpendGas(11, 12, 1, core.Nonce{Counter: 1}) - 1) * defaultGasPrice)},
						// send enough funds to cover spawn, but no spend
						&spendTx{11, 12, 1},
					},
					failed: map[int]error{1: core.ErrNoBalance},
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
						&spawnTx{0},
						&spendTx{0, 10, 100},
						&spendTx{0, 11, 100},
						&spendTx{0, 12, 100},
					},
					gasLimit: uint64(ref.estimateSpawnGas(0) +
						ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 1})),
					ineffective: []int{2, 3},
					expected: map[int]change{
						0:  spent{amount: 100 + ref.estimateSpawnGas(0) + ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 1})},
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
						&spawnTx{0},
						&spendTx{0, 10, uint64(ref.estimateSpawnGas(1)) - 1},
						&spawnTx{10},
						&spendTx{0, 11, 100},
					},
					gasLimit: uint64(ref.estimateSpawnGas(0) +
						ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 1}) +
						ref.estimateSpawnGas(1)),
					failed:      map[int]error{2: core.ErrNoBalance},
					ineffective: []int{3},
					expected: map[int]change{
						0: spent{amount: ref.estimateSpawnGas(1) - 1 +
							ref.estimateSpawnGas(0) +
							ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 1})},
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
						&spawnTx{0},
						spendTx{0, 11, 100}.withNonce(core.Nonce{Counter: 2}),
						spendTx{0, 10, 100}.withNonce(core.Nonce{Counter: 1}),
					},
					ineffective: []int{2},
					headers: map[int]struct{}{
						2: {},
					},
					expected: map[int]change{
						0: spawned{
							template: template,
							change:   spent{amount: 100 + defaultGasPrice*(ref.estimateSpawnGas(0)+ref.estimateSpendGas(0, 11, 100, core.Nonce{Counter: 2}))},
						},
						10: same{},
						11: earned{amount: 100},
					},
				},
				{
					txs: []testTx{
						spendTx{0, 10, 100}.withNonce(core.Nonce{Counter: 3}),
						spendTx{0, 12, 100}.withNonce(core.Nonce{Counter: 6}),
					},
					expected: map[int]change{
						0: spent{amount: 2*100 + defaultGasPrice*(ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 3})+
							ref.estimateSpendGas(0, 10, 100, core.Nonce{Counter: 6}))},
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
						&spawnTx{0},
					},
					rewards: []reward{{address: 10, share: 1}},
					expected: map[int]change{
						10: earned{amount: testBaseReward + ref.estimateSpawnGas(0)},
					},
				},
				{
					txs: []testTx{
						&spawnTx{10},
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
						10: earned{amount: testBaseReward / 2},
						11: earned{amount: testBaseReward / 2},
					},
				},
				{
					txs: []testTx{
						&spawnTx{0},
					},
					rewards: []reward{{address: 10, share: 0.5}, {address: 11, share: 0.5}},
					expected: map[int]change{
						10: earned{amount: (testBaseReward + ref.estimateSpawnGas(10)) / 2},
						11: earned{amount: (testBaseReward + ref.estimateSpawnGas(11)) / 2},
					},
				},
			},
		},
		{
			desc: "SkippedTransactionsNotRewarded",
			layers: []layertc{
				{
					txs: []testTx{
						&spawnTx{0},
						spendTx{0, 10, 100}.withNonce(core.Nonce{Counter: 5}),
					},
				},
				{
					txs: []testTx{
						spendTx{0, 10, 100}.withNonce(core.Nonce{Counter: 2}),
						spendTx{0, 11, 100}.withNonce(core.Nonce{Counter: 3}),
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
						&spawnTx{0},
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
						&spawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11) + ref.estimateSpendGas(11, 12, 1_000, core.Nonce{Counter: 1}))},
						&spawnTx{11},
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
						&spendTx{0, 11, uint64(ref.estimateSpendGas(11, 12, 1_000, core.Nonce{Counter: 2})) + 1_000},
						&spendTx{11, 12, 1_000},
					},
					expected: map[int]change{
						0:  spent{amount: ref.estimateSpendGas(11, 12, 1_000, core.Nonce{Counter: 2})*2 + 1_000, change: nonce{increased: 1}},
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
						&spawnTx{0},
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11)) - 1},
						&spawnTx{11},
					},
					failed: map[int]error{2: core.ErrNoBalance},
					expected: map[int]change{
						0: spent{amount: ref.estimateSpawnGas(11) - 1 +
							ref.estimateSpawnGas(0) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11)-1, core.Nonce{Counter: 1})},
						11: nonce{increased: 1},
					},
				},
				{
					txs: []testTx{
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11))},
						&spawnTx{11},
					},
					expected: map[int]change{
						0: spent{amount: ref.estimateSpawnGas(11) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11), core.Nonce{Counter: 2})},
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
						&spawnTx{0},
						&spawnTx{0},
					},
					failed: map[int]error{1: core.ErrSpawned},
					expected: map[int]change{
						0: spawned{
							template: template,
							change: nonce{
								increased: 2,
								change:    spent{amount: 2 * ref.estimateSpawnGas(0)},
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
						&spawnTx{0},
						// gas will be higher than fixed, but less than max gas
						&spendTx{0, 11, uint64(ref.estimateSpawnGas(11)) - 1},
						// it will cause this transaction to be failed
						&spawnTx{11},
					},
					gasLimit: uint64(ref.estimateSpawnGas(0) +
						ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11)-1, core.Nonce{Counter: 1}) +
						ref.estimateSpawnGas(11)),
					failed:  map[int]error{2: core.ErrNoBalance},
					rewards: []reward{{address: 20, share: 1}},
					expected: map[int]change{
						0: spent{amount: ref.estimateSpawnGas(0) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11)-1, core.Nonce{Counter: 1}) +
							ref.estimateSpawnGas(11) - 1},
						11: nonce{increased: 1},
						// fees from every transaction (including failed) + testBaseReward
						20: earned{amount: ref.estimateSpawnGas(0) +
							ref.estimateSpendGas(0, 11, ref.estimateSpawnGas(11)-1, core.Nonce{Counter: 1}) +
							ref.estimateSpawnGas(11) - 1 +
							testBaseReward},
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			tt := genTester(t)

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
						require.Equal(t, types.TransactionSuccess.String(), rst.Status.String())
					} else {
						require.Equal(t, types.TransactionFailure.String(), rst.Status.String())
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

func TestWallets(t *testing.T) {
	const (
		funded  = 10  // number of funded accounts, included in genesis
		total   = 100 // total number of accounts
		balance = 1_000_000_000

		defaultGasPrice = 1
	)
	t.Run("SingleSig", func(t *testing.T) {
		testWallet(t, wallet.TemplateAddress, defaultGasPrice, func(t *testing.T) *tester {
			return newTester(t).
				addSingleSig(funded).
				applyGenesisWithBalance(balance).
				addSingleSig(total - funded).
				withBaseReward(testBaseReward)
		})
	})
	t.Run("MultiSig13", func(t *testing.T) {
		const n = 3
		testWallet(t, multisig.TemplateAddress1, defaultGasPrice, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(funded, 1, n, multisig.TemplateAddress1).
				applyGenesisWithBalance(balance).
				addMultisig(total-funded, 1, n, multisig.TemplateAddress1).
				withBaseReward(testBaseReward)
		})
	})
	t.Run("MultiSig25", func(t *testing.T) {
		const n = 5
		testWallet(t, multisig.TemplateAddress2, defaultGasPrice, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(funded, 2, n, multisig.TemplateAddress2).
				applyGenesisWithBalance(balance).
				addMultisig(total-funded, 2, n, multisig.TemplateAddress2).
				withBaseReward(testBaseReward)
		})
	})
	t.Run("MultiSig310", func(t *testing.T) {
		const n = 10
		testWallet(t, multisig.TemplateAddress3, defaultGasPrice, func(t *testing.T) *tester {
			return newTester(t).
				addMultisig(funded, 3, n, multisig.TemplateAddress3).
				applyGenesisWithBalance(balance).
				addMultisig(total-funded, 3, n, multisig.TemplateAddress3).
				withBaseReward(testBaseReward)
		})
	})
}

func TestRandomTransfers(t *testing.T) {
	tt := newTester(t).withSeed(101).
		addSingleSig(10).
		addMultisig(10, 1, 3, multisig.TemplateAddress1).
		addMultisig(10, 2, 5, multisig.TemplateAddress2).
		addMultisig(10, 3, 10, multisig.TemplateAddress3).
		applyGenesis()

	skipped, _, err := tt.Apply(testContext(types.NewLayerID(1)),
		notVerified(tt.spawnAll()...), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)
	for i := 0; i < 1000; i++ {
		lid := types.NewLayerID(2).Add(uint32(i))
		skipped, _, err := tt.Apply(testContext(lid),
			notVerified(tt.randSpendN(20, 10)...), nil)
		require.NoError(tt, err)
		require.Empty(tt, skipped)
	}
}

func testValidation(t *testing.T, tt *tester, template core.Address) {
	skipped, _, err := tt.Apply(testContext(types.NewLayerID(1)),
		notVerified(tt.selfSpawn(0)), nil)
	require.NoError(tt, err)
	require.Empty(tt, skipped)

	firstAddress := tt.accounts[0].getAddress()
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
			tx:   tt.selfSpawn(1),
			header: &core.Header{
				Principal: tt.accounts[1].getAddress(),
				Method:    0,
				Template:  template,
				GasPrice:  1,
				MaxGas:    uint64(tt.estimateSpawnGas(1)),
			},
		},
		{
			desc: "Spend",
			tx:   tt.spend(0, 1, 100),
			header: &core.Header{
				Principal: tt.accounts[0].getAddress(),
				Method:    1,
				Template:  template,
				GasPrice:  1,
				Nonce:     core.Nonce{Counter: 1},
				MaxSpend:  100,
				MaxGas:    uint64(tt.estimateSpendGas(0, 1, 100, core.Nonce{Counter: 1})),
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

func TestValidation(t *testing.T) {
	t.Run("SingleSig", func(t *testing.T) {
		tt := newTester(t).
			addSingleSig(1).
			applyGenesis().
			addSingleSig(1)
		testValidation(t, tt, wallet.TemplateAddress)
	})
	t.Run("MultiSig13", func(t *testing.T) {
		tt := newTester(t).
			addMultisig(1, 1, 3, multisig.TemplateAddress1).
			applyGenesis().
			addMultisig(1, 1, 3, multisig.TemplateAddress1)
		testValidation(t, tt, multisig.TemplateAddress1)
	})
	t.Run("MultiSig25", func(t *testing.T) {
		tt := newTester(t).
			addMultisig(1, 2, 5, multisig.TemplateAddress2).
			applyGenesis().
			addMultisig(1, 2, 5, multisig.TemplateAddress2)
		testValidation(t, tt, multisig.TemplateAddress2)
	})
	t.Run("MultiSig310", func(t *testing.T) {
		tt := newTester(t).
			addMultisig(1, 3, 10, multisig.TemplateAddress3).
			applyGenesis().
			addMultisig(1, 3, 10, multisig.TemplateAddress3)
		testValidation(t, tt, multisig.TemplateAddress3)
	})
}

func FuzzParse(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		tt := newTester(t).addSingleSig(1).applyGenesis()
		req := tt.Validation(types.NewRawTx(data))
		req.Parse()
	})
}

func BenchmarkValidation(b *testing.B) {
	tt := newTester(b).addSingleSig(2).applyGenesis()
	skipped, _, err := tt.Apply(ApplyContext{Layer: types.NewLayerID(1)},
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

func TestStateHashFromUpdatedAccounts(t *testing.T) {
	tt := newTester(t).addSingleSig(10).applyGenesis()

	root, err := tt.GetStateRoot()
	require.NoError(t, err)
	require.Equal(t, types.Hash32{}, root)

	lid := types.NewLayerID(1)
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
	lid := types.NewLayerID(1)
	skipped, _, err := tt.Apply(ApplyContext{Layer: types.NewLayerID(1)},
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
