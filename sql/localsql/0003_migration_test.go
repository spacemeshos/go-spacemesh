package localsql

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func saveBuilderState(dir string, state *NIPostBuilderState) error {
	if err := save(filepath.Join(dir, builderFilename), state); err != nil {
		return fmt.Errorf("saving builder state: %w", err)
	}
	return nil
}

func addChallenge(db sql.Executor, nodeID types.NodeID, ch *types.NIPostChallenge) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(ch.PublishEpoch))
		stmt.BindInt64(3, int64(ch.Sequence))
		stmt.BindBytes(4, ch.PrevATXID.Bytes())
		stmt.BindBytes(5, ch.PositioningATX.Bytes())
		if ch.CommitmentATX != nil {
			stmt.BindBytes(6, ch.CommitmentATX.Bytes())
		} else {
			stmt.BindNull(6)
		}
		if ch.InitialPost != nil {
			stmt.BindInt64(7, int64(ch.InitialPost.Nonce))
			stmt.BindBytes(8, ch.InitialPost.Indices)
			stmt.BindInt64(9, int64(ch.InitialPost.Pow))
		} else {
			stmt.BindNull(7)
			stmt.BindNull(8)
			stmt.BindNull(9)
		}
	}
	if _, err := db.Exec(`
		insert into nipost (id, epoch, sequence, prev_atx, pos_atx, commit_atx,
			post_nonce, post_indices, post_pow)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert nipost challenge for %s pub-epoch %d: %w", nodeID, ch.PublishEpoch, err)
	}
	return nil
}

func Test_0003Migration_CompatibleSQL(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test1.db")
	db, err := Open("file:"+file,
		sql.WithMigration(New0003Migration(zaptest.NewLogger(t), t.TempDir(), nil)),
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

	ch := &types.NIPostChallenge{
		PublishEpoch:   1,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
	}
	err = addChallenge(db, nodeID, ch)
	require.NoError(t, err)
	state := &NIPostBuilderState{
		Challenge: ch.Hash(),
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	err = New0003Migration(zaptest.NewLogger(t), dataDir, nil).Apply(db)
	require.NoError(t, err)

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

func Test_0003Migration_Nipost_State_Challenge_Mismatch(t *testing.T) {
	dataDir := t.TempDir()

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

	ch := &types.NIPostChallenge{
		PublishEpoch:   1,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
	}
	err = addChallenge(db, nodeID, ch)
	require.NoError(t, err)
	state := &NIPostBuilderState{
		Challenge: types.RandomHash(),
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	observer, observedLogs := observer.New(zapcore.InfoLevel)
	logger := zap.New(observer)

	err = New0003Migration(logger, dataDir, nil).Apply(db)
	require.NoError(t, err)
	require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
	require.Equal(t, zapcore.WarnLevel, observedLogs.All()[0].Level)
	require.Contains(t, observedLogs.All()[0].Message, "challenge mismatch")

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

	ch := &types.NIPostChallenge{
		PublishEpoch:   1,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
	}
	err = addChallenge(db, nodeID, ch)
	require.NoError(t, err)

	state := &NIPostBuilderState{
		Challenge: ch.Hash(),
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

	err = New0003Migration(zaptest.NewLogger(t), dataDir, nil).Apply(db)
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
		Return([]byte("service1"))
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service2"))
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	err = New0003Migration(zaptest.NewLogger(t), dataDir, []PoetClient{poetClient1, poetClient2}).Apply(db)
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

func Test_0003Migration_Phase0_MainnetPoet2(t *testing.T) {
	dataDir := t.TempDir()

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

	ch := &types.NIPostChallenge{
		PublishEpoch:   1,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
	}
	err = addChallenge(db, nodeID, ch)
	require.NoError(t, err)
	mainnetPoet2, err := hex.DecodeString("f115c42343303b7b895083451653a6fee4e32429de57d16274ca579a7e791bc6")
	require.NoError(t, err)

	endTime := time.Now()
	state := &NIPostBuilderState{
		Challenge: ch.Hash(),
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
					ServiceID: mainnetPoet2,
				},
			},
		},
	}
	require.NoError(t, saveBuilderState(dataDir, state))
	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	observer, observedLogs := observer.New(zapcore.InfoLevel)
	logger := zap.New(observer)

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service1"))
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service2"))
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	err = New0003Migration(logger, dataDir, []PoetClient{poetClient1}).Apply(db)
	require.NoError(t, err)
	require.Equal(t, 2, observedLogs.Len(), "expected 1 log message")

	firstLog := observedLogs.FilterMessageSnippet("mainnet-poet-2.spacemesh.network").TakeAll()[0]
	require.Equal(t, zapcore.InfoLevel, firstLog.Level)
	require.Contains(t, firstLog.Message, "`mainnet-poet-2.spacemesh.network` has been retired")

	secondLog := observedLogs.All()[0]
	require.Equal(t, zapcore.InfoLevel, secondLog.Level)
	require.Contains(t, secondLog.Message, "PoET registration added to database")
	require.Equal(t, nodeID.ShortString(), secondLog.ContextMap()["node_id"])
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("service1")), secondLog.ContextMap()["poet_service_id"])
	require.Equal(t, "http://poet1.com", secondLog.ContextMap()["address"])
	require.Equal(t, "101", secondLog.ContextMap()["round_id"])
	require.Equal(t, endTime.UTC(), secondLog.ContextMap()["round_end"].(time.Time).UTC())

	_, err = db.Exec("select hash, address, round_id, round_end from poet_registration where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		},
		func(stmt *sql.Statement) bool {
			buf := make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)

			require.Equal(t, state.Challenge.Bytes(), buf)
			require.Equal(t, "http://poet1.com", stmt.ColumnText(1))
			require.Equal(t, state.PoetRequests[0].PoetRound.ID, stmt.ColumnText(2))
			require.Equal(t, endTime.Unix(), stmt.ColumnInt64(3))
			return true
		})
	require.NoError(t, err)

	require.NoFileExists(t, filepath.Join(dataDir, builderFilename))
}

func Test_0003Migration_Phase1_Complete(t *testing.T) {
	dataDir := t.TempDir()

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

	ch := &types.NIPostChallenge{
		PublishEpoch:   1,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
	}
	err = addChallenge(db, nodeID, ch)
	require.NoError(t, err)

	endTime := time.Now()
	state := &NIPostBuilderState{
		Challenge: ch.Hash(),
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

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service1"))
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service2"))
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	err = New0003Migration(zaptest.NewLogger(t), dataDir, []PoetClient{poetClient1, poetClient2}).Apply(db)
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

	cAtx := types.RandomATXID()
	ch := &types.NIPostChallenge{
		PublishEpoch:   1,
		Sequence:       2,
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  &cAtx,
		InitialPost: &types.Post{
			Nonce:   4,
			Indices: []byte{1, 2, 3},
			Pow:     7,
		},
	}
	err = addChallenge(db, nodeID, ch)
	require.NoError(t, err)

	endTime := time.Now()
	poetProofRef := types.PoetProofRef{
		4, 5, 6,
	}
	state := &NIPostBuilderState{
		Challenge:    ch.Hash(),
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

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service1"))
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service2"))
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	err = New0003Migration(zaptest.NewLogger(t), dataDir, []PoetClient{poetClient1, poetClient2}).Apply(db)
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

	ch := &types.NIPostChallenge{
		PublishEpoch:   1,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
	}
	err = addChallenge(db, nodeID, ch)
	require.NoError(t, err)

	endTime := time.Now()
	poetProofRef := types.PoetProofRef{
		4, 5, 6,
	}
	state := &NIPostBuilderState{
		Challenge:    ch.Hash(),
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

	ctrl := gomock.NewController(t)
	poetClient1 := NewMockPoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service1"))
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com")

	poetClient2 := NewMockPoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return([]byte("service2"))
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com")

	migration := New0003Migration(zaptest.NewLogger(t), dataDir, []PoetClient{poetClient1, poetClient2})

	tx, err := db.Tx(context.Background())
	require.NoError(t, err)

	require.NoError(t, migration.Apply(tx))
	require.NoError(t, migration.Rollback())
	require.NoError(t, tx.Release())

	require.FileExists(t, filepath.Join(dataDir, builderFilename))

	// rolling back again is no-op
	require.NoError(t, migration.Rollback())
}
