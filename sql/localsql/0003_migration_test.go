package localsql

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

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
				PoetServiceID: types.PoetServiceID{
					ServiceID: []byte("service1"),
				},
			},
			{
				PoetRound: &types.PoetRound{
					ID: "102",
				},
				PoetServiceID: types.PoetServiceID{
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

	state := &NIPostBuilderState{
		Challenge: types.Hash32{1, 2, 3},
		PoetRequests: []PoetRequest{
			{
				PoetRound: &types.PoetRound{
					ID:  "101",
					End: types.RoundEnd(time.Now()),
				},
				PoetServiceID: types.PoetServiceID{
					ServiceID: []byte("service1"),
				},
			},
			{
				PoetRound: &types.PoetRound{
					ID:  "102",
					End: types.RoundEnd(time.Now()),
				},
				PoetServiceID: types.PoetServiceID{
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
	poetClient1 := NewMockpoetClient(ctrl)
	poetClient1.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(types.PoetServiceID{ServiceID: []byte("service1")}, nil)
	poetClient1.EXPECT().Address().AnyTimes().Return("http://poet1.com", nil)

	poetClient2 := NewMockpoetClient(ctrl)
	poetClient2.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().
		Return(types.PoetServiceID{ServiceID: []byte("service2")}, nil)
	poetClient2.EXPECT().Address().AnyTimes().Return("http://poet2.com", nil)

	err = New0003Migration(dataDir, []poetClient{poetClient1, poetClient2}).Apply(db)
	require.NoError(t, err)

	// TODO(mafa): assert data in DB

	require.NoFileExists(t, filepath.Join(dataDir, builderFilename))
}

// TODO(mafa): add tests for phase 1
// TODO(mafa): add tests for phase 2
