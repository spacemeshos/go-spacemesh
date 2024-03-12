package internal

import (
	"context"
	"encoding/hex"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func Test_MergeDBs_InvalidTargetScheme(t *testing.T) {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(observer)
	tmpDst := t.TempDir()

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)

	db, err := localsql.Open("file:"+filepath.Join(tmpDst, localDbFile),
		sql.WithMigrations(migrations[:2]), // old schema
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = MergeDBs(context.Background(), logger, "", tmpDst)
	var schemaErr ErrInvalidSchemaVersion
	require.ErrorAs(t, err, &schemaErr)
	require.Equal(t, schemaErr.Expected, len(migrations))
	require.Equal(t, schemaErr.Actual, 2)

	require.Equal(t, 1, observedLogs.Len(), "Expected a warning log")
	require.Equal(t, observedLogs.All()[0].Message, "target database has an invalid schema version - aborting merge")
	require.Equal(t, observedLogs.All()[0].ContextMap()["db version"], int64(2))
	require.Equal(t, observedLogs.All()[0].ContextMap()["expected version"], int64(len(migrations)))
}

func Test_MergeDBs_TargetIsSupervised(t *testing.T) {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(observer)
	tmpDst := t.TempDir()

	err := os.MkdirAll(filepath.Join(tmpDst, keyDir), 0o700)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDst, keyDir, supervisedIDKeyFileName), []byte("key"), 0o600)
	require.NoError(t, err)

	db, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = MergeDBs(context.Background(), logger, "", tmpDst)
	require.ErrorIs(t, err, ErrSupervisedNode)

	require.Equal(t, 1, observedLogs.Len(), "Expected a warning log")
	require.Contains(t, observedLogs.All()[0].Message, "target appears to be a supervised node")
}

func Test_MergeDBs_InvalidSourcePath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDst := t.TempDir()

	db, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = MergeDBs(context.Background(), logger, "/invalid/source/path", tmpDst)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func Test_MergeDBs_InvalidSourceScheme(t *testing.T) {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(observer)
	tmpDst := t.TempDir()

	migrations, err := sql.LocalMigrations()
	require.NoError(t, err)

	db, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	tmpSrc := t.TempDir()
	db, err = localsql.Open("file:"+filepath.Join(tmpSrc, localDbFile),
		sql.WithMigrations(migrations[:2]), // old schema
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = MergeDBs(context.Background(), logger, tmpSrc, tmpDst)
	var schemaErr ErrInvalidSchemaVersion
	require.ErrorAs(t, err, &schemaErr)
	require.Equal(t, schemaErr.Expected, len(migrations))
	require.Equal(t, schemaErr.Actual, 2)

	require.Equal(t, 1, observedLogs.Len(), "Expected a warning log")
	require.Equal(t, observedLogs.All()[0].Message, "source database has an invalid schema version - aborting merge")
	require.Equal(t, observedLogs.All()[0].ContextMap()["db version"], int64(2))
	require.Equal(t, observedLogs.All()[0].ContextMap()["expected version"], int64(len(migrations)))
}

func Test_MergeDBs_SourceIsSupervised(t *testing.T) {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(observer)
	tmpDst := t.TempDir()

	db, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	tmpSrc := t.TempDir()

	err = os.MkdirAll(filepath.Join(tmpSrc, keyDir), 0o700)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpSrc, keyDir, supervisedIDKeyFileName), []byte("key"), 0o600)
	require.NoError(t, err)

	db, err = localsql.Open("file:" + filepath.Join(tmpSrc, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = MergeDBs(context.Background(), logger, tmpSrc, tmpDst)
	require.ErrorIs(t, err, ErrSupervisedNode)

	require.Equal(t, 1, observedLogs.Len(), "Expected a warning log")
	require.Contains(t, observedLogs.All()[0].Message, "source appears to be a supervised node")
}

func Test_MergeDBs_InvalidSourceKey(t *testing.T) {
	tmpDst := t.TempDir()

	db, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	tmpSrc := t.TempDir()
	err = os.MkdirAll(filepath.Join(tmpSrc, keyDir), 0o700)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpSrc, keyDir, "invalid.key"), []byte("key"), 0o600)
	require.NoError(t, err)

	db, err = localsql.Open("file:" + filepath.Join(tmpSrc, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = MergeDBs(context.Background(), zaptest.NewLogger(t), tmpSrc, tmpDst)
	require.ErrorContains(t, err, "not a valid key file")
	require.ErrorContains(t, err, "invalid.key")
}

func Test_MergeDBs_TargetKeyAlreadyExists(t *testing.T) {
	tmpDst := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDst, keyDir), 0o700)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(tmpDst, keyDir, "exists.key"), []byte("key"), 0o600)
	require.NoError(t, err)

	db, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	tmpSrc := t.TempDir()
	err = os.MkdirAll(filepath.Join(tmpSrc, keyDir), 0o700)
	require.NoError(t, err)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	key := make([]byte, hex.EncodedLen(len(signer.PrivateKey())))
	hex.Encode(key, signer.PrivateKey())
	err = os.WriteFile(filepath.Join(tmpSrc, keyDir, "exists.key"), key, 0o600)
	require.NoError(t, err)

	db, err = localsql.Open("file:" + filepath.Join(tmpSrc, localDbFile))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	err = MergeDBs(context.Background(), zaptest.NewLogger(t), tmpSrc, tmpDst)
	require.ErrorIs(t, err, fs.ErrExist)
	require.ErrorContains(t, err, "exists.key")
}

func Test_MergeDBs_Successful_Existing_Node(t *testing.T) {
	tmpDst := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDst, keyDir), 0o700)
	require.NoError(t, err)

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)

	key := make([]byte, hex.EncodedLen(len(sig1.PrivateKey())))
	hex.Encode(key, sig1.PrivateKey())
	err = os.WriteFile(filepath.Join(tmpDst, keyDir, "id1.key"), key, 0o600)
	require.NoError(t, err)

	dstDB, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)

	sig1Ch := &types.NIPostChallenge{
		PublishEpoch:   types.EpochID(rand.Uint32()),
		Sequence:       rand.Uint64(),
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
	}
	err = nipost.AddChallenge(dstDB, sig1.NodeID(), sig1Ch)
	require.NoError(t, err)

	sig1Post := nipost.Post{
		Nonce:    rand.Uint32(),
		Pow:      rand.Uint64(),
		Indices:  types.RandomBytes(32),
		NumUnits: rand.Uint32(),
	}
	err = nipost.AddInitialPost(dstDB, sig1.NodeID(), sig1Post)
	require.NoError(t, err)

	sig1Poet1 := nipost.PoETRegistration{
		ChallengeHash: sig1Ch.Hash(),
		Address:       "http://poet1.spacemesh.io",
		RoundID:       "1",
		RoundEnd:      time.Now().Round(time.Second),
	}
	sig1Poet2 := nipost.PoETRegistration{
		ChallengeHash: sig1Ch.Hash(),
		Address:       "http://poet2.spacemesh.io",
		RoundID:       "10",
		RoundEnd:      time.Now().Round(time.Second),
	}
	err = nipost.AddPoetRegistration(dstDB, sig1.NodeID(), sig1Poet1)
	require.NoError(t, err)
	err = nipost.AddPoetRegistration(dstDB, sig1.NodeID(), sig1Poet2)
	require.NoError(t, err)

	require.NoError(t, dstDB.Close())

	tmpSrc := t.TempDir()
	err = os.MkdirAll(filepath.Join(tmpSrc, keyDir), 0o700)
	require.NoError(t, err)

	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	key = make([]byte, hex.EncodedLen(len(sig2.PrivateKey())))
	hex.Encode(key, sig2.PrivateKey())
	err = os.WriteFile(filepath.Join(tmpSrc, keyDir, "id2.key"), key, 0o600)
	require.NoError(t, err)

	srcDB, err := localsql.Open("file:" + filepath.Join(tmpSrc, localDbFile))
	require.NoError(t, err)

	cAtx := types.RandomATXID()
	sig2Ch := &types.NIPostChallenge{
		PublishEpoch:   types.EpochID(rand.Uint32()),
		Sequence:       rand.Uint64(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  &cAtx,
		InitialPost: &types.Post{
			Nonce:   rand.Uint32(),
			Pow:     rand.Uint64(),
			Indices: types.RandomBytes(32),
		},
	}
	err = nipost.AddChallenge(srcDB, sig2.NodeID(), sig2Ch)
	require.NoError(t, err)

	sig2Post := nipost.Post{
		Nonce:    rand.Uint32(),
		Pow:      rand.Uint64(),
		Indices:  types.RandomBytes(32),
		NumUnits: rand.Uint32(),
	}
	err = nipost.AddInitialPost(srcDB, sig2.NodeID(), sig2Post)
	require.NoError(t, err)

	sig2Poet1 := nipost.PoETRegistration{
		ChallengeHash: sig2Ch.Hash(),
		Address:       "http://poet1.spacemesh.io",
		RoundID:       "1",
		RoundEnd:      time.Now().Round(time.Second),
	}
	sig2Poet2 := nipost.PoETRegistration{
		ChallengeHash: sig2Ch.Hash(),
		Address:       "http://poet2.spacemesh.io",
		RoundID:       "10",
		RoundEnd:      time.Now().Round(time.Second),
	}
	err = nipost.AddPoetRegistration(srcDB, sig2.NodeID(), sig2Poet1)
	require.NoError(t, err)
	err = nipost.AddPoetRegistration(srcDB, sig2.NodeID(), sig2Poet2)
	require.NoError(t, err)

	require.NoError(t, srcDB.Close())

	err = MergeDBs(context.Background(), zaptest.NewLogger(t), tmpSrc, tmpDst)
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(tmpDst, keyDir, "id1.key"))
	require.FileExists(t, filepath.Join(tmpDst, keyDir, "id2.key"))

	dstDB, err = localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	defer dstDB.Close()

	ch, err := nipost.Challenge(dstDB, sig1.NodeID())
	require.NoError(t, err)
	require.Equal(t, sig1Ch, ch)

	post, err := nipost.InitialPost(dstDB, sig1.NodeID())
	require.NoError(t, err)
	require.Equal(t, sig1Post, *post)

	poet1, err := nipost.PoetRegistrations(dstDB, sig1.NodeID())
	require.NoError(t, err)
	require.Equal(t, poet1[0], sig1Poet1)
	require.Equal(t, poet1[1], sig1Poet2)

	ch, err = nipost.Challenge(dstDB, sig2.NodeID())
	require.NoError(t, err)
	require.Equal(t, sig2Ch, ch)

	post, err = nipost.InitialPost(dstDB, sig2.NodeID())
	require.NoError(t, err)
	require.Equal(t, sig2Post, *post)

	poet2, err := nipost.PoetRegistrations(dstDB, sig2.NodeID())
	require.NoError(t, err)
	require.Equal(t, poet2[0], sig2Poet1)
	require.Equal(t, poet2[1], sig2Poet2)

	require.NoError(t, dstDB.Close())
}

func Test_MergeDBs_Successful_Empty_Dir(t *testing.T) {
	tmpDst := t.TempDir()

	tmpSrc := t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpSrc, keyDir), 0o700)
	require.NoError(t, err)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	key := make([]byte, hex.EncodedLen(len(sig.PrivateKey())))
	hex.Encode(key, sig.PrivateKey())
	err = os.WriteFile(filepath.Join(tmpSrc, keyDir, "id.key"), key, 0o600)
	require.NoError(t, err)

	srcDB, err := localsql.Open("file:" + filepath.Join(tmpSrc, localDbFile))
	require.NoError(t, err)

	cAtx := types.RandomATXID()
	sigCh := &types.NIPostChallenge{
		PublishEpoch:   types.EpochID(rand.Uint32()),
		Sequence:       rand.Uint64(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  &cAtx,
		InitialPost: &types.Post{
			Nonce:   rand.Uint32(),
			Pow:     rand.Uint64(),
			Indices: types.RandomBytes(32),
		},
	}
	err = nipost.AddChallenge(srcDB, sig.NodeID(), sigCh)
	require.NoError(t, err)

	sigPost := nipost.Post{
		Nonce:    rand.Uint32(),
		Pow:      rand.Uint64(),
		Indices:  types.RandomBytes(32),
		NumUnits: rand.Uint32(),
	}
	err = nipost.AddInitialPost(srcDB, sig.NodeID(), sigPost)
	require.NoError(t, err)

	sigPoet1 := nipost.PoETRegistration{
		ChallengeHash: sigCh.Hash(),
		Address:       "http://poet1.spacemesh.io",
		RoundID:       "1",
		RoundEnd:      time.Now().Round(time.Second),
	}
	sigPoet2 := nipost.PoETRegistration{
		ChallengeHash: sigCh.Hash(),
		Address:       "http://poet2.spacemesh.io",
		RoundID:       "10",
		RoundEnd:      time.Now().Round(time.Second),
	}
	err = nipost.AddPoetRegistration(srcDB, sig.NodeID(), sigPoet1)
	require.NoError(t, err)
	err = nipost.AddPoetRegistration(srcDB, sig.NodeID(), sigPoet2)
	require.NoError(t, err)

	require.NoError(t, srcDB.Close())

	err = MergeDBs(context.Background(), zaptest.NewLogger(t), tmpSrc, tmpDst)
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(tmpDst, keyDir, "id.key"))

	dstDB, err := localsql.Open("file:" + filepath.Join(tmpDst, localDbFile))
	require.NoError(t, err)
	defer dstDB.Close()

	ch, err := nipost.Challenge(dstDB, sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, sigCh, ch)

	post, err := nipost.InitialPost(dstDB, sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, sigPost, *post)

	poet, err := nipost.PoetRegistrations(dstDB, sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, poet[0], sigPoet1)
	require.Equal(t, poet[1], sigPoet2)

	require.NoError(t, dstDB.Close())
}
