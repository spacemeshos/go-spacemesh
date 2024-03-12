package internal

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

const (
	localDbFile = "local.sql"

	keyDir                  = "identities"
	supervisedIDKeyFileName = "local.key"
)

func MergeDBs(ctx context.Context, dbLog *zap.Logger, from, to string) error {
	// Open the target database
	var dstDB *localsql.Database
	var err error
	dstDB, err = openDB(dbLog, to)
	var schemaErr ErrInvalidSchemaVersion
	switch {
	case errors.As(err, &schemaErr):
		dbLog.Error("target database has an invalid schema version - aborting merge",
			zap.Int("db version", schemaErr.Actual),
			zap.Int("expected version", schemaErr.Expected),
		)
		return err
	case errors.Is(err, fs.ErrNotExist):
		// target database does not exist, create it
		dbLog.Info("target database does not exist, creating it", zap.String("path", to))
		if err := os.MkdirAll(to, 0o700); err != nil {
			return fmt.Errorf("create target directory: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(to, keyDir), 0o700); err != nil {
			return fmt.Errorf("create target key directory: %w", err)
		}

		dstDB, err = localsql.Open("file:"+filepath.Join(to, localDbFile),
			sql.WithLogger(dbLog),
		)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		// target database exists, check if there is at least one key in the target key directory
		// not named supervisedIDKeyFileName
		if err := checkIdentities(dbLog, to); err != nil {
			dbLog.Error("target appears to be a supervised node - only merging of remote smeshers is supported")
			return err
		}
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

	if err := checkIdentities(dbLog, from); err != nil {
		dbLog.Error("source appears to be a supervised node - only merging of remote smeshers is supported")
		return err
	}

	// copy files from `from` to `to`
	dir := filepath.Join(from, keyDir)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk directory at %s: %w", path, err)
		}

		// skip subdirectories and files in them
		if d.IsDir() && path != dir {
			return fs.SkipDir
		}

		// skip files that are not identity files
		if filepath.Ext(path) != ".key" {
			return nil
		}

		signer, err := signing.NewEdSigner(
			signing.FromFile(path),
		)
		if err != nil {
			return fmt.Errorf("not a valid key file %s: %w", d.Name(), err)
		}

		dstPath := filepath.Join(to, keyDir, d.Name())
		if _, err := os.Stat(dstPath); err == nil {
			return fmt.Errorf("identity file %s already exists: %w", d.Name(), fs.ErrExist)
		}

		dst := make([]byte, hex.EncodedLen(len(signer.PrivateKey())))
		hex.Encode(dst, signer.PrivateKey())
		err = os.WriteFile(dstPath, dst, 0o600)
		if err != nil {
			return fmt.Errorf("failed to write identity file: %w", err)
		}

		dbLog.Info("copied identity",
			zap.String("name", d.Name()),
		)
		return nil
	})
	if err != nil {
		return err
	}

	dbLog.Info("merging databases", zap.String("from", from), zap.String("to", to))
	err = dstDB.WithTx(ctx, func(tx *sql.Tx) error {
		enc := func(stmt *sql.Statement) {
			stmt.BindText(1, filepath.Join(from, localDbFile))
		}
		if _, err := tx.Exec("ATTACH DATABASE ?1 AS srcDB;", enc, nil); err != nil {
			return fmt.Errorf("attach source database: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO main.initial_post SELECT * FROM srcDB.initial_post;", nil, nil); err != nil {
			return fmt.Errorf("merge initial_post: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO main.challenge SELECT * FROM srcDB.challenge;", nil, nil); err != nil {
			return fmt.Errorf("merge challenge: %w", err)
		}
		if _, err := tx.Exec(
			"INSERT INTO main.poet_registration SELECT * FROM srcDB.poet_registration;", nil, nil,
		); err != nil {
			return fmt.Errorf("merge poet_registration: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO main.nipost SELECT * FROM srcDB.nipost;", nil, nil); err != nil {
			return fmt.Errorf("merge nipost: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("start transaction: %w", err)
	}

	if err := dstDB.Close(); err != nil {
		return fmt.Errorf("close target database: %w", err)
	}
	return nil
}

func openDB(dbLog *zap.Logger, path string) (*localsql.Database, error) {
	dbPath := filepath.Join(path, localDbFile)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("open database %s: %w", dbPath, err)
	}

	migrations, err := sql.LocalMigrations()
	if err != nil {
		return nil, fmt.Errorf("get local migrations: %w", err)
	}

	db, err := localsql.Open("file:"+dbPath,
		sql.WithLogger(dbLog),
		sql.WithMigrations([]sql.Migration{}), // do not migrate database when opening
	)
	if err != nil {
		return nil, fmt.Errorf("open source database %s: %w", dbPath, err)
	}

	// check if the source database has the right schema
	var version int
	_, err = db.Exec("PRAGMA user_version;", nil, func(stmt *sql.Statement) bool {
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
	return db, nil
}

func checkIdentities(dbLog *zap.Logger, path string) error {
	dir := filepath.Join(path, keyDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read target key directory: %w", err)
	}
	if slices.ContainsFunc(files, func(e fs.DirEntry) bool { return e.Name() == supervisedIDKeyFileName }) {
		return ErrSupervisedNode
	}
	return nil
}
