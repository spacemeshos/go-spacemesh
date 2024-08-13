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
	var dstDB sql.LocalDatabase
	var err error
	dstDB, err = openDB(dbLog, to)
	switch {
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
		defer dstDB.Close()
	case err != nil:
		return fmt.Errorf("open target database: %w", err)
	default:
		defer dstDB.Close()
		// target database exists, check if there is at least one key in the target key directory
		// not named supervisedIDKeyFileName
		if err := checkIdentities(dbLog, to); err != nil {
			switch {
			case errors.Is(err, ErrSupervisedNode):
				dbLog.Sugar().Errorf(
					"target appears to be a supervised node (found %s in %s)"+
						"- only merging to remote smeshers is supported",
					supervisedIDKeyFileName,
					filepath.Join(to, keyDir),
				)
			}
			return err
		}
	}

	// Open the source database
	srcDB, err := openDB(dbLog, from)
	if err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	if err := srcDB.Close(); err != nil {
		return fmt.Errorf("close source database: %w", err)
	}

	if err := checkIdentities(dbLog, from); err != nil {
		switch {
		case errors.Is(err, ErrSupervisedNode):
			dbLog.Sugar().Errorf(
				"source appears to be a supervised node (found %s in %s)"+
					" - only merging from remote smeshers is supported",
				supervisedIDKeyFileName,
				filepath.Join(from, keyDir),
			)
		}
		return err
	}

	fromKeyDir := filepath.Join(from, keyDir)
	toKeyDir := filepath.Join(to, keyDir)

	// check for name collisions
	toKeyDirFiles, err := os.ReadDir(toKeyDir)
	if err != nil {
		return fmt.Errorf("read target key directory: %w", err)
	}
	toKeyDirFiles = slices.DeleteFunc(toKeyDirFiles, func(e fs.DirEntry) bool {
		// skip files that are not identity files
		return filepath.Ext(e.Name()) != ".key"
	})
	fromKeyDirFiles, err := os.ReadDir(fromKeyDir)
	if err != nil {
		return fmt.Errorf("read source key directory: %w", err)
	}
	fromKeyDirFiles = slices.DeleteFunc(fromKeyDirFiles, func(e fs.DirEntry) bool {
		// skip files that are not identity files
		return filepath.Ext(e.Name()) != ".key"
	})
	for _, toFile := range toKeyDirFiles {
		for _, fromFile := range fromKeyDirFiles {
			if toFile.Name() == fromFile.Name() {
				return fmt.Errorf("identity file %s already exists: %w", toFile.Name(), fs.ErrExist)
			}
		}
	}

	// copy files from `from` to `to`
	err = filepath.WalkDir(fromKeyDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk directory at %s: %w", path, err)
		}

		// skip subdirectories and files in them
		if d.IsDir() && path != fromKeyDir {
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
	err = dstDB.WithTx(ctx, func(tx sql.Transaction) error {
		enc := func(stmt *sql.Statement) {
			stmt.BindText(1, filepath.Join(from, localDbFile))
		}
		if _, err := tx.Exec("ATTACH DATABASE ?1 AS srcDB;", enc, nil); err != nil {
			return fmt.Errorf("attach source database: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO main.post SELECT * FROM srcDB.post;", nil, nil); err != nil {
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

func openDB(dbLog *zap.Logger, path string) (sql.LocalDatabase, error) {
	dbPath := filepath.Join(path, localDbFile)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("stat source database %s: %w", dbPath, err)
	}

	db, err := localsql.Open("file:"+dbPath,
		sql.WithLogger(dbLog),
		sql.WithMigrationsDisabled(),
	)
	if err != nil {
		return nil, fmt.Errorf("open source database %s: %w", dbPath, err)
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
