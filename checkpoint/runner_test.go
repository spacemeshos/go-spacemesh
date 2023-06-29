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

const numEpochs = 4

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(2)
	res := m.Run()
	os.Exit(res)
}

var commitments = map[types.NodeID]*types.ATXID{
	types.BytesToNodeID([]byte("smesher1")): {1},
	types.BytesToNodeID([]byte("smesher2")): {2},
	types.BytesToNodeID([]byte("smesher3")): {3},
	types.BytesToNodeID([]byte("smesher4")): {4},
}

var vrfNonces = map[types.NodeID]types.VRFPostIndex{
	types.BytesToNodeID([]byte("smesher1")): 123,
	types.BytesToNodeID([]byte("smesher2")): 152,
	types.BytesToNodeID([]byte("smesher3")): 211,
	types.BytesToNodeID([]byte("smesher4")): 420,
}

var allAtxs = []*types.ActivationTx{
	// smesher 1 has had 7 ATXs, one in each epoch from 1 to 7
	newATX(types.ATXID{11}, &types.ATXID{1}, 1, 0, 123, types.BytesToNodeID([]byte("smesher1"))),
	newATX(types.ATXID{12}, nil, 2, 1, 0, types.BytesToNodeID([]byte("smesher1"))),
	newATX(types.ATXID{13}, nil, 3, 2, 0, types.BytesToNodeID([]byte("smesher1"))),
	newATX(types.ATXID{14}, nil, 4, 3, 0, types.BytesToNodeID([]byte("smesher1"))),
	newATX(types.ATXID{15}, nil, 5, 4, 0, types.BytesToNodeID([]byte("smesher1"))),
	newATX(types.ATXID{16}, nil, 6, 5, 0, types.BytesToNodeID([]byte("smesher1"))),
	newATX(types.ATXID{17}, nil, 7, 6, 0, types.BytesToNodeID([]byte("smesher1"))),

	// smesher 2 has had 1 ATX in epoch 7
	newATX(types.ATXID{27}, &types.ATXID{2}, 7, 0, 152, types.BytesToNodeID([]byte("smesher2"))),

	// smesher 4 has had 1 ATX in epoch 2
	newATX(types.ATXID{32}, &types.ATXID{3}, 2, 0, 211, types.BytesToNodeID([]byte("smesher3"))),

	// smesher 4 has had 1 ATX in epoch 3 and one in epoch 7
	newATX(types.ATXID{43}, &types.ATXID{4}, 4, 0, 420, types.BytesToNodeID([]byte("smesher4"))),
	newATX(types.ATXID{47}, nil, 7, 1, 0, types.BytesToNodeID([]byte("smesher4"))),
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

func expectedCheckpoint(t *testing.T, snapshot types.LayerID, numEpochs int) *checkpoint.Checkpoint {
	t.Helper()
	result := &checkpoint.Checkpoint{
		Version: "https://spacemesh.io/checkpoint.schema.json.1.0",
		Data: checkpoint.InnerData{
			CheckpointId: "snapshot-5",
		},
	}

	if numEpochs < 1 {
		require.Fail(t, "numEpochs must be at least 1")
	}

	firstEpoch := 7 - numEpochs
	if firstEpoch < 1 {
		firstEpoch = 1
	}

	// TODO (mafa): this isn't correct yet
	// at the moment the last n of every identity will be persisted
	// goal -> all atxs of last n epochs + last of every identity
	for _, atx := range allAtxs {
		if atx.PublishEpoch > types.EpochID(firstEpoch) {
			nonce := vrfNonces[atx.SmesherID]
			result.Data.Atxs = append(result.Data.Atxs, toShortAtx(newvATX(t, atx), commitments[atx.SmesherID], &nonce))
		}
	}

	accounts := make(map[types.Address]*types.Account)
	for _, account := range allAccounts {
		if account.Layer <= snapshot {
			if a, ok := accounts[account.Address]; !ok {
				accounts[account.Address] = account
			} else {
				if account.Layer > a.Layer {
					accounts[account.Address] = account
				}
			}
		}
	}

	for _, account := range accounts {
		result.Data.Accounts = append(result.Data.Accounts, checkpoint.Account{
			Address:  account.Address.Bytes(),
			Balance:  account.Balance,
			Nonce:    account.NextNonce,
			Template: account.TemplateAddress.Bytes(),
			State:    account.State,
		})
	}

	return result
}

func newATX(id types.ATXID, commitAtx *types.ATXID, epoch uint32, seq, vrfNonce uint64, nodeID types.NodeID) *types.ActivationTx {
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
	if vrfNonce != 0 {
		atx.VRFNonce = (*types.VRFPostIndex)(&vrfNonce)
	}
	atx.SmesherID = nodeID
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	return atx
}

func newvATX(t *testing.T, atx *types.ActivationTx) *types.VerifiedActivationTx {
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
		require.NoError(t, atxs.Add(db, newvATX(t, atx)))
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
			err = checkpoint.Generate(ctx, fs, db, dir, snapshot, 2)
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
			expected := expectedCheckpoint(t, snapshot, numEpochs)
			require.NoError(t, json.Unmarshal(persisted, &got))
			j, _ := json.Marshal(expected)
			require.Equalf(t, *expected, got, "expected: %s\ngot: %s", j, persisted)
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
				atx = newATX(types.ATXID{13}, nil, 2, 1, 11, types.BytesToNodeID([]byte("smesher1")))
			} else if tc.missingVrf {
				atx = newATX(types.ATXID{13}, &types.ATXID{11}, 2, 1, 0, types.BytesToNodeID([]byte("smesher1")))
			}
			createMesh(t, db, []*types.ActivationTx{atx}, allAccounts)

			fs := afero.NewMemMapFs()
			dir, err := afero.TempDir(fs, "", "Generate")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = checkpoint.Generate(ctx, fs, db, dir, snapshot, numEpochs)
			if tc.missingCommitment {
				require.ErrorContains(t, err, "atxs snapshot commitment")
			} else if tc.missingVrf {
				require.ErrorContains(t, err, "atxs snapshot nonce")
			}
		})
	}
}
