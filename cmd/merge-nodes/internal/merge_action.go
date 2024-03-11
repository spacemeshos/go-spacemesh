package internal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/natefinch/atomic"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

const (
	localDbFile = "local.sql"

	keyDir                  = "identities"
	supervisedIDKeyFileName = "local.key"
)

type ErrInvalidSchemaVersion struct {
	Expected int
	Actual   int
}

func (e ErrInvalidSchemaVersion) Error() string {
	return fmt.Sprintf("invalid schema version: expected %d got %d", e.Expected, e.Actual)
}

func MergeDBs(ctx context.Context, dbLog *zap.Logger, name, from, to string) error {
	// Open the target database
	dstDB, err := openDB(dbLog, to)
	var schemaErr ErrInvalidSchemaVersion
	switch {
	case errors.As(err, &schemaErr):
		dbLog.Error("target database has an invalid schema version - aborting merge",
			zap.Int("db version", schemaErr.Actual),
			zap.Int("expected version", schemaErr.Expected),
		)
		return err
	case err != nil:
		return err
	}
	defer dstDB.Close()

	// Open the source database
	srcDB, err := openDB(dbLog, from)
	switch {
	case errors.As(err, &schemaErr):
		dbLog.Error("source database has an invalid schema version - aborting merge",
			zap.Int("db version", schemaErr.Actual),
			zap.Int("expected version", schemaErr.Expected),
		)
		return err
	case err != nil:
		return err
	}
	if err := srcDB.Close(); err != nil {
		return fmt.Errorf("close source database: %w", err)
	}

	dstKey := filepath.Join(to, keyDir, name+".key")
	if _, err := os.Stat(dstKey); err == nil {
		return fmt.Errorf("destination key file %s: %w", dstKey, fs.ErrExist)
	}

	srcKey := filepath.Join(from, keyDir, supervisedIDKeyFileName)
	f, err := os.Open(srcKey)
	if err != nil {
		return fmt.Errorf("open source key file %s: %w", srcKey, err)
	}
	defer f.Close()

	dbLog.Info("merging databases", zap.String("from", from), zap.String("to", to))
	err = dstDB.WithTx(ctx, func(tx *sql.Tx) error {
		enc := func(stmt *sql.Statement) {
			stmt.BindText(1, filepath.Join(from, localDbFile))
		}
		if _, err := tx.Exec("ATTACH DATABASE ?1 AS srcDB;", enc, nil); err != nil {
			return fmt.Errorf("attach source database: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO initial_post SELECT * FROM srcDB.initial_post;", nil, nil); err != nil {
			return fmt.Errorf("merge initial_post: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO challenge SELECT * FROM srcDB.challenge;", nil, nil); err != nil {
			return fmt.Errorf("merge challenge: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO poet_registration SELECT * FROM srcDB.poet_registration;", nil, nil); err != nil {
			return fmt.Errorf("merge poet_registration: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO nipost SELECT * FROM srcDB.nipost;", nil, nil); err != nil {
			return fmt.Errorf("merge nipost: %w", err)
		}
		if err := atomic.WriteFile(dstKey, f); err != nil {
			return fmt.Errorf("write destination key file %s: %w", dstKey, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("start transaction: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close source key file %s: %w", srcKey, err)
	}
	if err := dstDB.Close(); err != nil {
		return fmt.Errorf("close target database: %w", err)
	}
	return nil
}

func openDB(dbLog *zap.Logger, path string) (*localsql.Database, error) {
	dbPath := filepath.Join(path, localDbFile)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("open source database %s: %w", dbPath, err)
	}

	migrations, err := sql.LocalMigrations()
	if err != nil {
		return nil, fmt.Errorf("get local migrations: %w", err)
	}

	srcDB, err := localsql.Open("file:"+dbPath,
		sql.WithLogger(dbLog),
		sql.WithMigrations([]sql.Migration{}), // do not migrate database when opening
	)
	if err != nil {
		return nil, fmt.Errorf("open source database %s: %w", dbPath, err)
	}

	// check if the source database has the right schema
	var version int
	_, err = srcDB.Exec("PRAGMA user_version;", nil, func(stmt *sql.Statement) bool {
		version = stmt.ColumnInt(0)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("get source database schema for %s: %w", dbPath, err)
	}
	if version != len(migrations) {
		return nil, ErrInvalidSchemaVersion{
			Expected: len(migrations),
			Actual:   version,
		}
	}
	return srcDB, nil
}
