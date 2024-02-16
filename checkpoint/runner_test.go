package checkpoint_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(2)
	res := m.Run()
	os.Exit(res)
}

var allAtxs = map[types.NodeID][]*types.ActivationTx{
	// smesher 1 has 7 ATXs, one in each epoch from 1 to 7
	types.BytesToNodeID([]byte("smesher1")): {
		newAtx(types.ATXID{17}, nil, 7, 6, 0, types.BytesToNodeID([]byte("smesher1"))),
		newAtx(types.ATXID{16}, nil, 6, 5, 0, types.BytesToNodeID([]byte("smesher1"))),
		newAtx(types.ATXID{15}, nil, 5, 4, 0, types.BytesToNodeID([]byte("smesher1"))),
		newAtx(types.ATXID{14}, nil, 4, 3, 0, types.BytesToNodeID([]byte("smesher1"))),
		newAtx(types.ATXID{13}, nil, 3, 2, 0, types.BytesToNodeID([]byte("smesher1"))),
		newAtx(types.ATXID{12}, nil, 2, 1, 0, types.BytesToNodeID([]byte("smesher1"))),
		newAtx(types.ATXID{11}, &types.ATXID{1}, 1, 0, 123, types.BytesToNodeID([]byte("smesher1"))),
	},

	// smesher 2 has 1 ATX in epoch 7
	types.BytesToNodeID([]byte("smesher2")): {
		newAtx(types.ATXID{27}, &types.ATXID{2}, 7, 0, 152, types.BytesToNodeID([]byte("smesher2"))),
	},

	// smesher 3 has 1 ATX in epoch 2
	types.BytesToNodeID([]byte("smesher3")): {
		newAtx(types.ATXID{32}, &types.ATXID{3}, 2, 0, 211, types.BytesToNodeID([]byte("smesher3"))),
	},

	// smesher 4 has 1 ATX in epoch 3 and one in epoch 7
	types.BytesToNodeID([]byte("smesher4")): {
		newAtx(types.ATXID{47}, nil, 7, 1, 0, types.BytesToNodeID([]byte("smesher4"))),
		newAtx(types.ATXID{43}, &types.ATXID{4}, 4, 0, 420, types.BytesToNodeID([]byte("smesher4"))),
	},
}

var allAccounts = []*types.Account{
	{
		Layer:           types.LayerID(0),
		Address:         types.Address{1, 1},
		NextNonce:       1,
		Balance:         1300,
		TemplateAddress: &types.Address{2},
		State:           []byte("state10"),
	},
	{
		Layer:           types.LayerID(1),
		Address:         types.Address{1, 1},
		NextNonce:       4,
		Balance:         3111,
		TemplateAddress: &types.Address{2},
		State:           []byte("state11"),
	},
	{
		Layer:           types.LayerID(5),
		Address:         types.Address{1, 1},
		NextNonce:       5,
		Balance:         111,
		TemplateAddress: &types.Address{2},
		State:           []byte("state15"),
	},
	{
		Layer:           types.LayerID(2),
		Address:         types.Address{2, 2},
		NextNonce:       1,
		Balance:         300,
		TemplateAddress: &types.Address{2},
		State:           []byte("state22"),
	},
	{
		Layer:           types.LayerID(4),
		Address:         types.Address{2, 2},
		NextNonce:       14,
		Balance:         311,
		TemplateAddress: &types.Address{2},
		State:           []byte("state24"),
	},
	{
		Layer:           types.LayerID(6),
		Address:         types.Address{2, 2},
		NextNonce:       15,
		Balance:         111,
		TemplateAddress: &types.Address{2},
		State:           []byte("state26"),
	},
	{
		Layer:           types.LayerID(5),
		Address:         types.Address{3, 3},
		NextNonce:       1,
		Balance:         124,
		TemplateAddress: &types.Address{3},
		State:           []byte("state35"),
	},
	{
		Layer:           types.LayerID(7),
		Address:         types.Address{4, 4},
		NextNonce:       1,
		Balance:         31,
		TemplateAddress: &types.Address{3},
		State:           []byte("state47"),
	},
}

func expectedCheckpoint(t *testing.T, snapshot types.LayerID, numAtxs int) *types.Checkpoint {
	t.Helper()

	request, err := json.Marshal(&pb.CheckpointStreamRequest{
		SnapshotLayer: uint32(snapshot),
		NumAtxs:       uint32(numAtxs),
	})
	require.NoError(t, err)

	result := &types.Checkpoint{
		Command: fmt.Sprintf(checkpoint.CommandString, request),
		Version: "https://spacemesh.io/checkpoint.schema.json.1.0",
		Data: types.InnerData{
			CheckpointId: "snapshot-5",
		},
	}

	if numAtxs < 2 {
		require.Fail(t, "numEpochs must be at least 2")
	}

	atxData := make([]types.AtxSnapshot, 0, numAtxs*len(allAtxs))
	for _, atxs := range allAtxs {
		n := len(atxs)
		if n > numAtxs {
			n = numAtxs
		}
		for i := 0; i < n; i++ {
			atxData = append(
				atxData,
				toShortAtx(newvAtx(t, atxs[i]), atxs[len(atxs)-1].CommitmentATX, atxs[len(atxs)-1].VRFNonce),
			)
		}
	}

	result.Data.Atxs = atxData

	accounts := make(map[types.Address]*types.Account)
	for _, account := range allAccounts {
		if account.Layer <= snapshot {
			a, ok := accounts[account.Address]
			switch {
			case !ok:
				accounts[account.Address] = account
			case account.Layer > a.Layer:
				accounts[account.Address] = account
			}
		}
	}

	for _, account := range accounts {
		result.Data.Accounts = append(result.Data.Accounts, types.AccountSnapshot{
			Address:  account.Address.Bytes(),
			Balance:  account.Balance,
			Nonce:    account.NextNonce,
			Template: account.TemplateAddress.Bytes(),
			State:    account.State,
		})
	}

	return result
}

func newAtx(
	id types.ATXID,
	commitAtx *types.ATXID,
	epoch uint32,
	seq, vrfnonce uint64,
	nodeID types.NodeID,
) *types.ActivationTx {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:  types.EpochID(epoch),
				Sequence:      seq,
				CommitmentATX: commitAtx,
			},
			NIPost: &types.NIPost{
				PostMetadata: &types.PostMetadata{
					Challenge: types.RandomBytes(5),
				},
			},
			NumUnits: 2,
			Coinbase: types.Address{1, 2, 3},
		},
	}
	atx.SetID(id)
	if vrfnonce != 0 {
		atx.VRFNonce = (*types.VRFPostIndex)(&vrfnonce)
	}
	atx.SmesherID = nodeID
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	return atx
}

func newvAtx(tb testing.TB, atx *types.ActivationTx) *types.VerifiedActivationTx {
	vatx, err := atx.Verify(1111, 12)
	require.NoError(tb, err)
	return vatx
}

func toShortAtx(v *types.VerifiedActivationTx, cmt *types.ATXID, nonce *types.VRFPostIndex) types.AtxSnapshot {
	return types.AtxSnapshot{
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

func createMesh(t *testing.T, db *sql.Database, miners map[types.NodeID][]*types.ActivationTx, accts []*types.Account) {
	for _, vatxs := range miners {
		for _, atx := range vatxs {
			require.NoError(t, atxs.Add(db, newvAtx(t, atx)))
		}
	}

	for _, it := range accts {
		require.NoError(t, accounts.Update(db, it))
	}

	// smesher 5 is malicious and equivocated in epoch 7
	bad := types.BytesToNodeID([]byte("smesher5"))
	require.NoError(t, atxs.Add(db, newvAtx(t, newAtx(types.ATXID{83}, &types.ATXID{27}, 7, 0, 113, bad))))
	require.NoError(t, atxs.Add(db, newvAtx(t, newAtx(types.ATXID{97}, &types.ATXID{16}, 7, 0, 113, bad))))
	require.NoError(t, identities.SetMalicious(db, bad, []byte("bad"), time.Now()))
}

func TestRunner_Generate(t *testing.T) {
	tcs := []struct {
		desc    string
		atxes   map[types.NodeID][]*types.ActivationTx
		numAtxs int
		accts   []*types.Account
		fail    bool
	}{
		{
			desc:    "all good, 2 atxs",
			atxes:   allAtxs,
			numAtxs: 2,
			accts:   allAccounts,
		},
		{
			desc:    "all good, 4 atxs",
			atxes:   allAtxs,
			numAtxs: 4,
			accts:   allAccounts,
		},
		{
			desc:    "all good, 7 atxs",
			atxes:   allAtxs,
			numAtxs: 7,
			accts:   allAccounts,
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
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			snapshot := types.LayerID(5)
			createMesh(t, db, tc.atxes, tc.accts)

			fs := afero.NewMemMapFs()
			dir, err := afero.TempDir(fs, "", "Generate")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = checkpoint.Generate(ctx, fs, db, dir, snapshot, tc.numAtxs)
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			fname := checkpoint.SelfCheckpointFilename(dir, snapshot)
			persisted, err := afero.ReadFile(fs, fname)
			require.NoError(t, err)
			require.NoError(t, checkpoint.ValidateSchema(persisted))
			var got types.Checkpoint
			expected := expectedCheckpoint(t, snapshot, tc.numAtxs)
			require.NoError(t, json.Unmarshal(persisted, &got))

			require.True(t, cmp.Equal(
				*expected,
				got,
				cmpopts.EquateEmpty(),
				cmpopts.SortSlices(func(a, b types.AtxSnapshot) bool { return bytes.Compare(a.ID, b.ID) < 0 }),
				cmpopts.SortSlices(
					func(a, b types.AccountSnapshot) bool { return bytes.Compare(a.Address, b.Address) < 0 },
				),
			), cmp.Diff(*expected, got))
		})
	}
}

func TestRunner_Generate_Error(t *testing.T) {
	const numEpochs = 2

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
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			snapshot := types.LayerID(5)
			var atx *types.ActivationTx
			if tc.missingCommitment {
				atx = newAtx(types.ATXID{13}, nil, 2, 1, 11, types.BytesToNodeID([]byte("smesher1")))
			} else if tc.missingVrf {
				atx = newAtx(types.ATXID{13}, &types.ATXID{11}, 2, 1, 0, types.BytesToNodeID([]byte("smesher1")))
			}
			createMesh(t, db, map[types.NodeID][]*types.ActivationTx{
				types.BytesToNodeID([]byte("smesher1")): {atx},
			}, allAccounts)

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
