package localsql

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Test_0003Migration_CompatibleSQL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test1.db")
	db, err := Open("file:"+file,
		sql.WithMigration(New0003Migration(t.TempDir(), nil)),
	)
	require.NoError(t, err)

	var sqls1 []string
	_, err = db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		sqls1 = append(sqls1, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	file = filepath.Join(t.TempDir(), "test2.db")
	db, err = Open("file:" + file)
	require.NoError(t, err)

	var sqls2 []string
	_, err = db.Exec("SELECT sql FROM sqlite_schema;", nil, func(stmt *sql.Statement) bool {
		sql := stmt.ColumnText(0)
		sql = strings.Join(strings.Fields(sql), " ") // remove whitespace
		sqls2 = append(sqls2, sql)
		return true
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.Equal(t, sqls1, sqls2)
}

func Test_0003Migration_BeforePhase0(t *testing.T) {
	dataDir := t.TempDir()

	state := &NIPostBuilderState{}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	nodeID := types.RandomNodeID()
	nonce := uint64(1024)
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:   nodeID.Bytes(),
		NumUnits: 8,
		Nonce:    &nonce,
	})
	require.NoError(t, err)

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:2]
	db := InMemory(
		sql.WithMigrations(migrations),
		sql.WithMigration(New0003Migration(dataDir, nil)),
	)

	_, err = db.Exec("select count(*) from poet_registration where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, func(stmt *sql.Statement) bool {
			count := int(stmt.ColumnInt64(0))
			require.Equal(t, 0, count)
			return true
		})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, builderFilename))
}

func Test_0003Migration_Phase0_missing_poet_client(t *testing.T) {
	dataDir := t.TempDir()

	state := &NIPostBuilderState{
		PoetRequests: []PoetRequest{
			{
				PoetRound: &types.PoetRound{
					ID: "101",
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service1"),
				},
			},
			{
				PoetRound: &types.PoetRound{
					ID: "102",
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service2"),
				},
			},
		},
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	nodeID := types.RandomNodeID()
	nonce := uint64(1024)
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:   nodeID.Bytes(),
		NumUnits: 8,
		Nonce:    &nonce,
	})
	require.NoError(t, err)

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:2]
	db := InMemory(
		sql.WithMigrations(migrations),
	)

	err = New0003Migration(dataDir, nil).Apply(db)
	require.ErrorContains(t, err, "no poet client found")
	require.FileExists(t, filepath.Join(dataDir, builderFilename))
}

func Test_0003Migration_Phase0_Complete(t *testing.T) {
	dataDir := t.TempDir()

	endTime := time.Now()
	state := &NIPostBuilderState{
		Challenge: types.Hash32{1, 2, 3},
		PoetRequests: []PoetRequest{
			{
				PoetRound: &types.PoetRound{
					ID:  "101",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service1"),
				},
			},
			{
				PoetRound: &types.PoetRound{
					ID:  "102",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service2"),
				},
			},
		},
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	nodeID := types.RandomNodeID()
	nonce := uint64(1024)
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:   nodeID.Bytes(),
		NumUnits: 8,
		Nonce:    &nonce,
	})
	require.NoError(t, err)

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:2]
	db := InMemory(
		sql.WithMigrations(migrations),
	)

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service1")})
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service2")})
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	err = New0003Migration(dataDir, []PoetClient{poetClient1, poetClient2}).Apply(db)
	require.NoError(t, err)

	address := []string{
		"http://poet1.com",
		"http://poet2.com",
	}
	i := 0 // index for db rows
	_, err = db.Exec("select hash, address, round_id, round_end from poet_registration where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		},
		func(stmt *sql.Statement) bool {
			buf := make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)

			require.Equal(t, state.Challenge.Bytes(), buf)
			require.Equal(t, address[i], stmt.ColumnText(1))
			require.Equal(t, state.PoetRequests[i].PoetRound.ID, stmt.ColumnText(2))
			require.Equal(t, endTime.Unix(), stmt.ColumnInt64(3))
			i++
			return true
		})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, builderFilename))
}

func Test_0003Migration_Phase1_Complete(t *testing.T) {
	dataDir := t.TempDir()

	endTime := time.Now()
	state := &NIPostBuilderState{
		Challenge: types.Hash32{1, 2, 3},
		PoetProofRef: types.PoetProofRef{
			4, 5, 6,
		},
		NIPost: &types.NIPost{
			Membership: types.MerkleProof{
				Nodes:     []types.Hash32{types.RandomHash(), types.RandomHash()},
				LeafIndex: 1,
			},
		},
		PoetRequests: []PoetRequest{
			{
				PoetRound: &types.PoetRound{
					ID:  "101",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service1"),
				},
			},
			{
				PoetRound: &types.PoetRound{
					ID:  "102",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service2"),
				},
			},
		},
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	nodeID := types.RandomNodeID()
	nonce := uint64(1024)
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:   nodeID.Bytes(),
		NumUnits: 8,
		Nonce:    &nonce,
	})
	require.NoError(t, err)

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:2]
	db := InMemory(
		sql.WithMigrations(migrations),
	)

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service1")})
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service2")})
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(101))
		stmt.BindInt64(3, int64(5))
		stmt.BindBytes(4, types.RandomATXID().Bytes())
		stmt.BindBytes(5, types.RandomATXID().Bytes())
	}
	_, err = db.Exec(`
		insert into nipost (id, epoch, sequence, prev_atx, pos_atx)
		values (?1, ?2, ?3, ?4, ?5);`, enc, nil,
	)
	require.NoError(t, err)

	err = New0003Migration(dataDir, []PoetClient{poetClient1, poetClient2}).Apply(db)
	require.NoError(t, err)

	address := []string{
		"http://poet1.com",
		"http://poet2.com",
	}
	i := 0 // index for db rows
	_, err = db.Exec("select hash, address, round_id, round_end from poet_registration where id = ?1;",
		func(s *sql.Statement) {
			s.BindBytes(1, nodeID.Bytes())
		},
		func(s *sql.Statement) bool {
			buf := make([]byte, s.ColumnLen(0))
			s.ColumnBytes(0, buf)

			require.Equal(t, state.Challenge.Bytes(), buf)
			require.Equal(t, address[i], s.ColumnText(1))
			require.Equal(t, state.PoetRequests[i].PoetRound.ID, s.ColumnText(2))
			require.Equal(t, endTime.Unix(), s.ColumnInt64(3))
			i++
			return true
		})
	require.NoError(t, err)

	_, err = db.Exec("select poet_proof_ref, poet_proof_membership from challenge where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		},
		func(stmt *sql.Statement) bool {
			buf := make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)

			require.Equal(t, state.PoetProofRef[:], buf)

			buf = make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, buf)
			require.Equal(t, codec.MustEncode(&state.NIPost.Membership), buf)
			return true
		})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, builderFilename))
}

func Test_0003Migration_Phase2_Complete(t *testing.T) {
	dataDir := t.TempDir()

	endTime := time.Now()
	poetProofRef := types.PoetProofRef{
		4, 5, 6,
	}
	state := &NIPostBuilderState{
		Challenge:    types.Hash32{1, 2, 3},
		PoetProofRef: poetProofRef,
		NIPost: &types.NIPost{
			Post: &types.Post{
				Pow:     7,
				Indices: []byte{1, 2, 3},
				Nonce:   4,
			},
			PostMetadata: &types.PostMetadata{
				Challenge:     poetProofRef[:],
				LabelsPerUnit: 1024,
			},
			Membership: types.MerkleProof{
				Nodes:     []types.Hash32{types.RandomHash(), types.RandomHash()},
				LeafIndex: 1,
			},
		},
		PoetRequests: []PoetRequest{
			{
				PoetRound: &types.PoetRound{
					ID:  "101",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service1"),
				},
			},
			{
				PoetRound: &types.PoetRound{
					ID:  "102",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service2"),
				},
			},
		},
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	nodeID := types.RandomNodeID()
	nonce := uint64(1024)
	numUnits := uint32(8)
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:   nodeID.Bytes(),
		NumUnits: numUnits,
		Nonce:    &nonce,
	})
	require.NoError(t, err)

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:2]
	db := InMemory(
		sql.WithMigrations(migrations),
	)

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service1")})
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service2")})
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(101))
		stmt.BindInt64(3, int64(5))
		stmt.BindBytes(4, types.RandomATXID().Bytes())
		stmt.BindBytes(5, types.RandomATXID().Bytes())
	}
	_, err = db.Exec(`
		insert into nipost (id, epoch, sequence, prev_atx, pos_atx)
		values (?1, ?2, ?3, ?4, ?5);`, enc, nil,
	)
	require.NoError(t, err)

	err = New0003Migration(dataDir, []PoetClient{poetClient1, poetClient2}).Apply(db)
	require.NoError(t, err)

	address := []string{
		"http://poet1.com",
		"http://poet2.com",
	}
	i := 0 // index for db rows
	_, err = db.Exec("select hash, address, round_id, round_end from poet_registration where id = ?1;",
		func(s *sql.Statement) {
			s.BindBytes(1, nodeID.Bytes())
		},
		func(s *sql.Statement) bool {
			buf := make([]byte, s.ColumnLen(0))
			s.ColumnBytes(0, buf)

			require.Equal(t, state.Challenge.Bytes(), buf)
			require.Equal(t, address[i], s.ColumnText(1))
			require.Equal(t, state.PoetRequests[i].PoetRound.ID, s.ColumnText(2))
			require.Equal(t, endTime.Unix(), s.ColumnInt64(3))
			i++
			return true
		})
	require.NoError(t, err)

	_, err = db.Exec("select poet_proof_ref, poet_proof_membership from challenge where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		},
		func(stmt *sql.Statement) bool {
			buf := make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)

			require.Equal(t, state.PoetProofRef[:], buf)

			buf = make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, buf)
			require.Equal(t, codec.MustEncode(&state.NIPost.Membership), buf)
			return true
		})
	require.NoError(t, err)

	_, err = db.Exec(`
	select post_nonce, post_indices, post_pow, num_units, vrf_nonce,
		poet_proof_membership, poet_proof_ref, labels_per_unit
	from nipost where id = ?1`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}, func(stmt *sql.Statement) bool {
		require.Equal(t, state.NIPost.Post.Nonce, uint32(stmt.ColumnInt64(0)))

		buf := make([]byte, stmt.ColumnLen(1))
		stmt.ColumnBytes(1, buf)

		require.Equal(t, state.NIPost.Post.Indices, buf)
		require.Equal(t, state.NIPost.Post.Pow, uint64(stmt.ColumnInt64(2)))
		require.Equal(t, numUnits, uint32(stmt.ColumnInt64(3)))
		require.Equal(t, nonce, uint64(stmt.ColumnInt64(4)))

		buf = make([]byte, stmt.ColumnLen(5))
		stmt.ColumnBytes(5, buf)

		require.Equal(t, codec.MustEncode(&state.NIPost.Membership), buf)

		buf = make([]byte, stmt.ColumnLen(6))
		stmt.ColumnBytes(6, buf)

		require.Equal(t, state.PoetProofRef[:], buf)
		require.Equal(t, state.NIPost.PostMetadata.LabelsPerUnit, uint64(stmt.ColumnInt64(7)))
		return true
	})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, builderFilename))
}

func Test_0003Migration_Rollback(t *testing.T) {
	dataDir := t.TempDir()

	endTime := time.Now()
	poetProofRef := types.PoetProofRef{
		4, 5, 6,
	}
	state := &NIPostBuilderState{
		Challenge:    types.Hash32{1, 2, 3},
		PoetProofRef: poetProofRef,
		NIPost: &types.NIPost{
			Post: &types.Post{
				Pow:     7,
				Indices: []byte{1, 2, 3},
				Nonce:   4,
			},
			PostMetadata: &types.PostMetadata{
				Challenge:     poetProofRef[:],
				LabelsPerUnit: 1024,
			},
			Membership: types.MerkleProof{
				Nodes:     []types.Hash32{types.RandomHash(), types.RandomHash()},
				LeafIndex: 1,
			},
		},
		PoetRequests: []PoetRequest{
			{
				PoetRound: &types.PoetRound{
					ID:  "101",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service1"),
				},
			},
			{
				PoetRound: &types.PoetRound{
					ID:  "102",
					End: types.RoundEnd(endTime),
				},
				PoetServiceID: PoetServiceID{
					ServiceID: []byte("service2"),
				},
			},
		},
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	nodeID := types.RandomNodeID()
	nonce := uint64(1024)
	numUnits := uint32(8)
	err := initialization.SaveMetadata(dataDir, &shared.PostMetadata{
		NodeId:   nodeID.Bytes(),
		NumUnits: numUnits,
		Nonce:    &nonce,
	})
	require.NoError(t, err)

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Order() < migrations[j].Order() })
	migrations = migrations[:2]
	db := InMemory(
		sql.WithMigrations(migrations),
	)

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service1")})
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(PoetServiceID{ServiceID: []byte("service2")})
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(101))
		stmt.BindInt64(3, int64(5))
		stmt.BindBytes(4, types.RandomATXID().Bytes())
		stmt.BindBytes(5, types.RandomATXID().Bytes())
	}
	_, err = db.Exec(`
		insert into nipost (id, epoch, sequence, prev_atx, pos_atx)
		values (?1, ?2, ?3, ?4, ?5);`, enc, nil,
	)
	require.NoError(t, err)

	migration := New0003Migration(dataDir, []PoetClient{poetClient1, poetClient2})

	tx, err := db.Tx(context.Background())
	require.NoError(t, err)

	require.NoError(t, migration.Apply(tx))
	require.NoError(t, migration.Rollback())
	require.NoError(t, tx.Release())

	require.FileExists(t, filepath.Join(dataDir, builderFilename))
}
