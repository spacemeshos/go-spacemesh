package checkpoint_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(1)
	res := m.Run()
	os.Exit(res)
}

var allAtxs = []*types.ActivationTx{
	newatx(types.ATXID{12}, &types.ATXID{1}, 1, 0, 123, []byte("smesher1")),
	newatx(types.ATXID{13}, nil, 2, 1, 0, []byte("smesher1")),
	newatx(types.ATXID{14}, nil, 3, 2, 0, []byte("smesher1")),
	newatx(types.ATXID{15}, nil, 4, 3, 0, []byte("smesher1")),
	newatx(types.ATXID{23}, &types.ATXID{2}, 3, 0, 152, []byte("smesher2")),
	newatx(types.ATXID{32}, &types.ATXID{3}, 2, 0, 211, []byte("smesher3")),
	newatx(types.ATXID{34}, nil, 4, 1, 0, []byte("smesher3")),
}

var allAccounts = []*types.Account{
	{Layer: types.LayerID(0), Address: types.Address{1, 1}, NextNonce: 1, Balance: 1300, TemplateAddress: &types.Address{2}, State: []byte("state10")},
	{Layer: types.LayerID(1), Address: types.Address{1, 1}, NextNonce: 4, Balance: 3111, TemplateAddress: &types.Address{2}, State: []byte("state11")},
	{Layer: types.LayerID(5), Address: types.Address{1, 1}, NextNonce: 5, Balance: 111, TemplateAddress: &types.Address{2}, State: []byte("state15")},
	{Layer: types.LayerID(2), Address: types.Address{2, 2}, NextNonce: 1, Balance: 300, TemplateAddress: &types.Address{2}, State: []byte("state22")},
	{Layer: types.LayerID(4), Address: types.Address{2, 2}, NextNonce: 14, Balance: 311, TemplateAddress: &types.Address{2}, State: []byte("state24")},
	{Layer: types.LayerID(6), Address: types.Address{2, 2}, NextNonce: 15, Balance: 111, TemplateAddress: &types.Address{2}, State: []byte("state26")},
	{Layer: types.LayerID(5), Address: types.Address{3, 3}, NextNonce: 1, Balance: 124, TemplateAddress: &types.Address{3}, State: []byte("state35")},
	{Layer: types.LayerID(7), Address: types.Address{4, 4}, NextNonce: 1, Balance: 31, TemplateAddress: &types.Address{3}, State: []byte("state47")},
}

func expectedCheckpoint(t *testing.T) *checkpoint.Checkpoint {
	return &checkpoint.Checkpoint{
		Version: "https://spacemesh.io/checkpoint.schema.json.1.0",
		Data: checkpoint.InnerData{
			CheckpointId: "snapshot-5",
			Atxs: []checkpoint.ShortAtx{
				toShortAtx(newvatx(t, allAtxs[3]), allAtxs[0].CommitmentATX, allAtxs[0].VRFNonce),
				toShortAtx(newvatx(t, allAtxs[2]), allAtxs[0].CommitmentATX, allAtxs[0].VRFNonce),
				toShortAtx(newvatx(t, allAtxs[4]), allAtxs[4].CommitmentATX, allAtxs[4].VRFNonce),
				toShortAtx(newvatx(t, allAtxs[6]), allAtxs[5].CommitmentATX, allAtxs[5].VRFNonce),
				toShortAtx(newvatx(t, allAtxs[5]), allAtxs[5].CommitmentATX, allAtxs[5].VRFNonce),
			},
			Accounts: []checkpoint.Account{
				{types.Address{1, 1}.Bytes(), 111, 5, types.Address{2}.Bytes(), []byte("state15")},
				{types.Address{2, 2}.Bytes(), 311, 14, types.Address{2}.Bytes(), []byte("state24")},
				{types.Address{3, 3}.Bytes(), 124, 1, types.Address{3}.Bytes(), []byte("state35")},
			},
		},
	}
}

func newatx(id types.ATXID, commitAtx *types.ATXID, epoch uint32, seq, vrfnonce uint64, pubkey []byte) *types.ActivationTx {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:  types.EpochID(epoch),
				Sequence:      seq,
				CommitmentATX: commitAtx,
			},
			NumUnits: 2,
			Coinbase: types.Address{1, 2, 3},
		},
	}
	atx.SetID(id)
	if vrfnonce != 0 {
		atx.VRFNonce = (*types.VRFPostIndex)(&vrfnonce)
	}
	atx.SmesherID = types.BytesToNodeID(pubkey)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	return atx
}

func newvatx(t *testing.T, atx *types.ActivationTx) *types.VerifiedActivationTx {
	vatx, err := atx.Verify(1111, 12)
	require.NoError(t, err)
	return vatx
}

func toShortAtx(v *types.VerifiedActivationTx, cmt *types.ATXID, nonce *types.VRFPostIndex) checkpoint.ShortAtx {
	return checkpoint.ShortAtx{
		ID:             v.ID().Bytes(),
		Epoch:          v.PublishEpoch.Uint32(),
		CommitmentAtx:  cmt.Bytes(),
		VrfNonce:       uint64(*nonce),
		NumUnits:       v.NumUnits,
		BaseTickHeight: v.BaseTickHeight(),
		TickCount:      v.TickCount(),
		PublicKey:      v.SmesherID.Bytes(),
		Sequence:       v.Sequence,
		Coinbase:       v.Coinbase.Bytes(),
	}
}

func createMesh(t *testing.T, db *sql.Database, atxes []*types.ActivationTx, accts []*types.Account) {
	for _, atx := range atxes {
		require.NoError(t, atxs.Add(db, newvatx(t, atx)))
	}

	for _, it := range accts {
		require.NoError(t, accounts.Update(db, it))
	}
}

func TestRunner_Generate(t *testing.T) {
	tcs := []struct {
		desc  string
		atxes []*types.ActivationTx
		accts []*types.Account
		fail  bool
	}{
		{
			desc:  "all good",
			atxes: allAtxs,
			accts: allAccounts,
		},
		{
			desc:  "no atxs",
			accts: allAccounts,
			fail:  true,
		},
		{
			desc:  "no accounts",
			atxes: allAtxs,
			fail:  true,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			snapshot := types.LayerID(5)
			createMesh(t, db, tc.atxes, tc.accts)

			fs := afero.NewMemMapFs()
			dir, err := afero.TempDir(fs, "", "Generate")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = checkpoint.Generate(ctx, fs, db, dir, snapshot)
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			fname := checkpoint.SelfCheckpointFilename(dir, snapshot)
			persisted, err := afero.ReadFile(fs, fname)
			require.NoError(t, err)
			require.NoError(t, checkpoint.ValidateSchema(persisted))
			var got checkpoint.Checkpoint
			expected := expectedCheckpoint(t)
			require.NoError(t, json.Unmarshal(persisted, &got))
			require.Equal(t, *expected, got)
		})
	}
}

func TestRunner_Generate_Error(t *testing.T) {
	tcs := []struct {
		desc              string
		missingVrf        bool
		missingCommitment bool
	}{
		{
			desc:       "no vrf nonce",
			missingVrf: true,
		},
		{
			desc:              "no commitment atx",
			missingCommitment: true,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			snapshot := types.LayerID(5)
			var atx *types.ActivationTx
			if tc.missingCommitment {
				atx = newatx(types.ATXID{13}, nil, 2, 1, 11, []byte("smesher1"))
			} else if tc.missingVrf {
				atx = newatx(types.ATXID{13}, &types.ATXID{11}, 2, 1, 0, []byte("smesher1"))
			}
			createMesh(t, db, []*types.ActivationTx{atx}, allAccounts)

			fs := afero.NewMemMapFs()
			dir, err := afero.TempDir(fs, "", "Generate")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = checkpoint.Generate(ctx, fs, db, dir, snapshot)
			if tc.missingCommitment {
				require.ErrorContains(t, err, "atxs snapshot commitment")
			} else if tc.missingVrf {
				require.ErrorContains(t, err, "atxs snapshot nonce")
			}
		})
	}
}
