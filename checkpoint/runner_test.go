package checkpoint_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)
	res := m.Run()
	os.Exit(res)
}

type createAtxOpt func(*types.ActivationTx)

func withSmesherID(nid types.NodeID) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.SmesherID = nid
	}
}

func withPublishEpoch(epoch types.EpochID) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.PublishEpoch = epoch
	}
}

func withSequence(seq uint64) createAtxOpt {
	return func(atx *types.ActivationTx) {
		atx.Sequence = seq
		atx.PublishEpoch = types.EpochID(uint32(seq + 1))
	}
}

func newAtx(opts ...createAtxOpt) (*types.VerifiedActivationTx, error) {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PrevATXID: types.RandomATXID(),
			},
			Coinbase: types.GenerateAddress(types.RandomBytes(20)),
			NumUnits: 2,
		},
	}
	for _, opt := range opts {
		opt(atx)
	}
	if atx.Sequence == 0 {
		atx.CommitmentATX = &types.ATXID{1, 2, 3}
		nonce := types.VRFPostIndex(11)
		atx.VRFNonce = &nonce
	}
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	return atx.Verify(0, 1)
}

func genAtxSeq(t *testing.T, nid types.NodeID, n int) []*types.VerifiedActivationTx {
	var seq []*types.VerifiedActivationTx
	for i := 0; i < n; i++ {
		vatx, err := newAtx(withSmesherID(nid), withSequence(uint64(i)), withPublishEpoch(types.EpochID(i+1)))
		require.NoError(t, err)
		seq = append(seq, vatx)
	}
	return seq
}

func genAccountSeq(address types.Address, n int) []*types.Account {
	var seq []*types.Account
	for i := 1; i <= n; i++ {
		seq = append(seq, &types.Account{
			Address: address,
			Layer:   types.LayerID(uint32(i)),
			Balance: uint64(i),
		})
	}
	return seq
}

func toShortAtx(v *types.VerifiedActivationTx, cmt *types.ATXID, nonce *types.VRFPostIndex) checkpoint.ShortAtx {
	return checkpoint.ShortAtx{
		ID:             hex.EncodeToString(v.ID().Bytes()),
		Epoch:          v.PublishEpoch.Uint32(),
		CommitmentAtx:  hex.EncodeToString(cmt.Bytes()),
		VrfNonce:       uint64(*nonce),
		NumUnits:       v.NumUnits,
		BaseTickHeight: v.BaseTickHeight(),
		TickCount:      v.TickCount(),
		PublicKey:      hex.EncodeToString(v.SmesherID.Bytes()),
		Sequence:       v.Sequence,
		Coinbase:       hex.EncodeToString(v.Coinbase.Bytes()),
	}
}

func TestRunner_Generate(t *testing.T) {
	db := sql.InMemory()
	addresses := []types.Address{{1, 1}, {2, 2}, {3, 3}}
	lids := []int{10, 7, 20}
	for i, address := range addresses {
		for _, update := range genAccountSeq(address, lids[i]) {
			require.NoError(t, accounts.Update(db, update))
		}
	}

	miners := []types.NodeID{{1, 1, 1}, {2, 2, 2}, {3, 3, 3}}
	epochs := []int{6, 3, 10}
	expected := map[types.NodeID][]checkpoint.ShortAtx{}
	for i, nid := range miners {
		var first *types.VerifiedActivationTx
		for j, atx := range genAtxSeq(t, nid, epochs[i]) {
			require.NoError(t, atxs.Add(db, atx))
			if j == 0 {
				first = atx
			}
			if j >= epochs[i]-2 {
				expected[nid] = append(expected[nid], toShortAtx(atx, first.CommitmentATX, first.VRFNonce))
			}
		}
	}

	fs := afero.NewMemMapFs()
	dir, err := afero.TempDir(fs, "", "Generate")
	require.NoError(t, err)
	r := checkpoint.NewRunner(db,
		checkpoint.WithFilesystem(fs),
		checkpoint.WithLogger(logtest.New(t)),
		checkpoint.WithDataDir(dir),
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := r.Generate(ctx, 15, 18)
	require.NoError(t, err)
	require.NoError(t, checkpoint.ValidateSchema(data))

	snapshot := types.LayerID(15)
	restore := types.LayerID(18)
	persisted, err := afero.ReadFile(fs, checkpoint.SelfCheckpointFilename(dir, snapshot, restore))
	require.NoError(t, err)
	require.True(t, bytes.Equal(data, persisted))

	var cpdata checkpoint.Checkpoint
	require.NoError(t, json.Unmarshal(data, &cpdata))
	require.Equal(t, cpdata.Version, cpdata.Version)
	require.EqualValues(t, restore, cpdata.Data.Restore)
	require.Len(t, cpdata.Data.Atxs, len(miners)*2)
	got := map[types.NodeID][]checkpoint.ShortAtx{}
	for _, atx := range cpdata.Data.Atxs {
		decoded, err := hex.DecodeString(atx.PublicKey)
		require.NoError(t, err)
		nid := types.BytesToNodeID(decoded)
		got[nid] = append(got[nid], atx)
	}
	for nid, shortAtxes := range got {
		require.ElementsMatch(t, expected[nid], shortAtxes)
	}

	require.Len(t, cpdata.Data.Accounts, len(addresses))
	for i, acct := range cpdata.Data.Accounts {
		require.Equal(t, hex.EncodeToString(addresses[i].Bytes()), acct.Address)
		// balance is set to last update layer.
		require.LessOrEqual(t, acct.Balance, uint64(15))
	}
}
