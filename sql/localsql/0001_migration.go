package localsql

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/natefinch/atomic"
	"github.com/spacemeshos/post/initialization"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func New0001Migration(dataDir string) *migration0001 {
	return &migration0001{dataDir: dataDir}
}

type migration0001 struct {
	dataDir string
}

func (migration0001) Name() string {
	return "initial"
}

func (migration0001) Order() int {
	return 1
}

func (m migration0001) Apply(db sql.Executor) error {
	_, err := db.Exec(`CREATE TABLE nipost (
		id            CHAR(32) PRIMARY KEY,
		epoch         UNSIGNED INT NOT NULL,
		sequence      UNSIGNED INT NOT NULL,
		prev_atx      CHAR(32) NOT NULL,
		pos_atx       CHAR(32) NOT NULL,
		commit_atx    CHAR(32),
		post_nonce    UNSIGNED INT,
		post_indices  VARCHAR,
		post_pow      UNSIGNED LONG INT
	) WITHOUT ROWID;`, nil, nil)
	if err != nil {
		return fmt.Errorf("create nipost table: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE initial_post (
		id            CHAR(32) PRIMARY KEY,
		post_nonce    UNSIGNED INT NOT NULL,
		post_indices  VARCHAR NOT NULL,
		post_pow      UNSIGNED LONG INT NOT NULL,

		commit_atx    CHAR(32)
	) WITHOUT ROWID;`, nil, nil)
	if err != nil {
		return fmt.Errorf("create initial_post table: %w", err)
	}

	// move post and nipost challenge to db
	if err := m.movePostToDb(db, m.dataDir); err != nil {
		return fmt.Errorf("move post to db: %w", err)
	}
	if err := m.moveNipostChallengeToDb(db, m.dataDir); err != nil {
		return fmt.Errorf("move nipost challenge to db: %w", err)
	}

	return nil
}

func (m migration0001) Rollback() error {
	filename := filepath.Join(m.dataDir, postFilename)
	backupName := fmt.Sprintf("%s.bak", filename)
	if err := atomic.ReplaceFile(backupName, filename); err != nil {
		return fmt.Errorf("rolling back post: %w", err)
	}

	filename = filepath.Join(m.dataDir, challengeFilename)
	backupName = fmt.Sprintf("%s.bak", filename)
	if err := atomic.ReplaceFile(backupName, filename); err != nil {
		return fmt.Errorf("rolling back nipost challenge: %w", err)
	}

	return nil
}

func (migration0001) movePostToDb(db sql.Executor, dataDir string) error {
	post, err := loadPost(dataDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil // no post file, nothing to do
	case err != nil:
		return fmt.Errorf("loading post: %w", err)
	}

	meta, err := initialization.LoadMetadata(dataDir)
	if err != nil {
		return fmt.Errorf("load post metadata: %w", err)
	}

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, meta.NodeId)
		stmt.BindInt64(2, int64(post.Nonce))
		stmt.BindBytes(3, post.Indices)
		stmt.BindInt64(4, int64(post.Pow))
		stmt.BindBytes(5, meta.CommitmentAtxId)
	}
	if _, err := db.Exec(`
		insert into initial_post (
			id, post_nonce, post_indices, post_pow, commit_atx
		) values (?1, ?2, ?3, ?4, ?5);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert initial post for %s: %w", types.BytesToNodeID(meta.NodeId).ShortString(), err)
	}
	return discardPost(dataDir)
}

func (migration0001) moveNipostChallengeToDb(db sql.Executor, dataDir string) error {
	ch, err := loadNipostChallenge(dataDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil // no challenge file, nothing to do
	case err != nil:
		return fmt.Errorf("loading nipost challenge: %w", err)
	}

	meta, err := initialization.LoadMetadata(dataDir)
	if err != nil {
		return fmt.Errorf("load post metadata: %w", err)
	}

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, meta.NodeId)
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
		return fmt.Errorf("insert nipost challenge for %s pub-epoch %d: %w",
			types.BytesToNodeID(meta.NodeId).ShortString(), ch.PublishEpoch, err,
		)
	}
	return discardNipostChallenge(dataDir)
}
