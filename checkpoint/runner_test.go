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

type miner struct {
	atxs             []*types.ActivationTx
	malfeasanceProof []byte
}

var allMiners = []miner{
	// smesher 1 has 7 ATXs, one in each epoch from 1 to 7
	{
		atxs: []*types.ActivationTx{
			newAtx(types.ATXID{17}, types.ATXID{16}, nil, 7, 6, 123, []byte("smesher1")),
			newAtx(types.ATXID{16}, types.ATXID{15}, nil, 6, 5, 123, []byte("smesher1")),
			newAtx(types.ATXID{15}, types.ATXID{14}, nil, 5, 4, 123, []byte("smesher1")),
			newAtx(types.ATXID{14}, types.ATXID{13}, nil, 4, 3, 123, []byte("smesher1")),
			newAtx(types.ATXID{13}, types.ATXID{12}, nil, 3, 2, 123, []byte("smesher1")),
			newAtx(types.ATXID{12}, types.ATXID{11}, nil, 2, 1, 123, []byte("smesher1")),
			newAtx(types.ATXID{11}, types.EmptyATXID, &types.ATXID{1}, 1, 0, 123, []byte("smesher1")),
		},
	},

	// smesher 2 has 1 ATX in epoch 7
	{
		atxs: []*types.ActivationTx{
			newAtx(types.ATXID{27}, types.EmptyATXID, &types.ATXID{2}, 7, 0, 152, []byte("smesher2")),
		},
	},

	// smesher 3 has 1 ATX in epoch 2
	{
		atxs: []*types.ActivationTx{
			newAtx(types.ATXID{32}, types.EmptyATXID, &types.ATXID{3}, 2, 0, 211, []byte("smesher3")),
		},
	},

	// smesher 4 has 1 ATX in epoch 3 and one in epoch 7
	{
		atxs: []*types.ActivationTx{
			newAtx(types.ATXID{47}, types.ATXID{43}, nil, 7, 1, 420, []byte("smesher4")),
			newAtx(types.ATXID{43}, types.EmptyATXID, &types.ATXID{4}, 4, 0, 420, []byte("smesher4")),
		},
	},

	// smesher 5 is malicious and equivocated in epoch 7
	{
		atxs: []*types.ActivationTx{
			newAtx(types.ATXID{83}, types.EmptyATXID, &types.ATXID{27}, 7, 0, 113, []byte("smesher5")),
			newAtx(types.ATXID{97}, types.EmptyATXID, &types.ATXID{16}, 7, 0, 113, []byte("smesher5")),
		},
		malfeasanceProof: []byte("im bad"),
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

func expectedCheckpoint(t testing.TB, snapshot types.LayerID, numAtxs int, miners []miner) *types.Checkpoint {
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

	atxData := make([]types.AtxSnapshot, 0, numAtxs*len(miners))
	for _, miner := range miners {
		if len(miner.malfeasanceProof) > 0 {
			continue
		}
		atxs := miner.atxs
		n := len(atxs)
		if n > numAtxs {
			n = numAtxs
		}
		for i := 0; i < n; i++ {
			atxData = append(
				atxData,
				asAtxSnapshot(atxs[i], atxs[len(atxs)-1].CommitmentATX),
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
	id, prevID types.ATXID,
	commitAtx *types.ATXID,
	epoch uint32,
	seq, vrfnonce uint64,
	nodeID []byte,
) *types.ActivationTx {
	atx := &types.ActivationTx{
		PublishEpoch:  types.EpochID(epoch),
		Sequence:      seq,
		CommitmentATX: commitAtx,
		PrevATXID:     prevID,
		NumUnits:      2,
		Coinbase:      types.Address{1, 2, 3},
		TickCount:     1,
		SmesherID:     types.BytesToNodeID(nodeID),
		VRFNonce:      types.VRFPostIndex(vrfnonce),
	}
	atx.SetID(id)
	atx.SetReceived(time.Now().Local())
	return atx
}

func asAtxSnapshot(v *types.ActivationTx, cmt *types.ATXID) types.AtxSnapshot {
	return types.AtxSnapshot{
		ID:             v.ID().Bytes(),
		Epoch:          v.PublishEpoch.Uint32(),
		CommitmentAtx:  cmt.Bytes(),
		VrfNonce:       uint64(v.VRFNonce),
		NumUnits:       v.NumUnits,
		BaseTickHeight: v.BaseTickHeight,
		TickCount:      v.TickCount,
		PublicKey:      v.SmesherID.Bytes(),
		Sequence:       v.Sequence,
		Coinbase:       v.Coinbase.Bytes(),
		Units:          map[types.NodeID]uint32{v.SmesherID: v.NumUnits},
	}
}

func createMesh(t testing.TB, db *sql.Database, miners []miner, accts []*types.Account) {
	t.Helper()
	for _, miner := range miners {
		for _, atx := range miner.atxs {
			require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
			require.NoError(t, atxs.SetUnits(db, atx.ID(), atx.SmesherID, atx.NumUnits))
		}
		if proof := miner.malfeasanceProof; len(proof) > 0 {
			require.NoError(t, identities.SetMalicious(db, miner.atxs[0].SmesherID, proof, time.Now()))
		}
	}

	for _, it := range accts {
		require.NoError(t, accounts.Update(db, it))
	}
}

func TestRunner_Generate(t *testing.T) {
	t.Parallel()
	tcs := []struct {
		desc    string
		miners  []miner
		numAtxs int
		accts   []*types.Account
	}{
		{
			desc:    "all good, 2 atxs",
			miners:  allMiners,
			numAtxs: 2,
			accts:   allAccounts,
		},
		{
			desc:    "all good, 4 atxs",
			miners:  allMiners,
			numAtxs: 4,
			accts:   allAccounts,
		},
		{
			desc:    "all good, 7 atxs",
			miners:  allMiners,
			numAtxs: 7,
			accts:   allAccounts,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			db := sql.InMemory()
			snapshot := types.LayerID(5)
			createMesh(t, db, tc.miners, tc.accts)

			fs := afero.NewMemMapFs()
			dir, err := afero.TempDir(fs, "", "Generate")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = checkpoint.Generate(ctx, fs, db, dir, snapshot, tc.numAtxs)
			require.NoError(t, err)
			fname := checkpoint.SelfCheckpointFilename(dir, snapshot)
			persisted, err := afero.ReadFile(fs, fname)
			require.NoError(t, err)
			require.NoError(t, checkpoint.ValidateSchema(persisted))
			var got types.Checkpoint
			expected := expectedCheckpoint(t, snapshot, tc.numAtxs, tc.miners)
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
	t.Parallel()
	t.Run("no commitment atx", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		snapshot := types.LayerID(5)

		atx := newAtx(types.ATXID{13}, types.EmptyATXID, nil, 2, 1, 11, types.RandomNodeID().Bytes())
		createMesh(t, db, []miner{{atxs: []*types.ActivationTx{atx}}}, allAccounts)

		fs := afero.NewMemMapFs()
		dir, err := afero.TempDir(fs, "", "Generate")
		require.NoError(t, err)
		err = checkpoint.Generate(context.Background(), fs, db, dir, snapshot, 2)
		require.ErrorContains(t, err, "atxs snapshot commitment")
	})
	t.Run("no atxs", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		snapshot := types.LayerID(5)
		createMesh(t, db, nil, allAccounts)

		fs := afero.NewMemMapFs()
		dir, err := afero.TempDir(fs, "", "Generate")
		require.NoError(t, err)

		err = checkpoint.Generate(context.Background(), fs, db, dir, snapshot, 2)
		require.Error(t, err)
	})
	t.Run("no accounts", func(t *testing.T) {
		t.Parallel()
		db := sql.InMemory()
		snapshot := types.LayerID(5)
		createMesh(t, db, allMiners, nil)

		fs := afero.NewMemMapFs()
		dir, err := afero.TempDir(fs, "", "Generate")
		require.NoError(t, err)

		err = checkpoint.Generate(context.Background(), fs, db, dir, snapshot, 2)
		require.Error(t, err)
	})
}
