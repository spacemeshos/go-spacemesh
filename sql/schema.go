package sql

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
)

const (
	SchemaPath        = "schema/schema.sql"
	UpdatedSchemaPath = "schema/schema.sql.updated"
)

// LoadDBSchemaScript retrieves the database schema as text.
func LoadDBSchemaScript(db Executor, ignoreRx string) (string, error) {
	var (
		err   error
		ignRx *regexp.Regexp
		sb    strings.Builder
	)
	if ignoreRx != "" {
		ignRx, err = regexp.Compile(ignoreRx)
		if err != nil {
			return "", fmt.Errorf("error compiling table ignore regexp %q: %w",
				ignoreRx, err)
		}
	}
	version, err := version(db)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&sb, "PRAGMA user_version = %d;\n", version)
	if _, err = db.Exec(
		// Type is either 'index' or 'table', we want tables to go first
		`select tbl_name, sql || ';' from sqlite_master
                 where sql is not null
                 order by tbl_name, type desc, name`,
		nil, func(st *Statement) bool {
			if ignRx == nil || !ignRx.MatchString(st.ColumnText(0)) {
				fmt.Fprintln(&sb, st.ColumnText(1))
			}
			return true
		}); err != nil {
		return "", fmt.Errorf("error retrieving DB schema: %w", err)
	}
	return sb.String(), nil
}

// Schema represents database schema.
type Schema struct {
	Script        string
	Migrations    MigrationList
	skipMigration map[int]struct{}
}

// Diff diffs the database schema against the actual schema.
// If there's no differences, it returns an empty string.
func (s *Schema) Diff(actualScript string) string {
	return cmp.Diff(s.Script, actualScript)
}

// WriteToFile writes the schema to the corresponding updated schema file.
func (s *Schema) WriteToFile(basedir string) error {
	path := filepath.Join(basedir, UpdatedSchemaPath)
	if err := os.WriteFile(path, []byte(s.Script), 0o777); err != nil {
		return fmt.Errorf("error writing schema file %s: %w", path, err)
	}
	return nil
}

// SkipMigrations skips the specified migrations.
func (s *Schema) SkipMigrations(i ...int) {
	if s.skipMigration == nil {
		s.skipMigration = make(map[int]struct{})
	}
	for _, index := range i {
		s.skipMigration[index] = struct{}{}
	}
}

// Apply applies the schema to the database.
func (s *Schema) Apply(db *Database) error {
	return db.WithTx(context.Background(), func(tx *Tx) error {
		scanner := bufio.NewScanner(strings.NewReader(s.Script))
		scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			if i := bytes.Index(data, []byte(";")); i >= 0 {
				return i + 1, data[0 : i+1], nil
			}
			return 0, nil, nil
		})
		for scanner.Scan() {
			if _, err := tx.Exec(scanner.Text(), nil, nil); err != nil {
				return fmt.Errorf("exec %s: %w", scanner.Text(), err)
			}
		}
		return nil
	})
}

// Migrate performs database migration. In case if migrations are disabled, the database
// version is checked but no migrations are run, and if the database is too old and
// migrations are disabled, an error is returned.
func (s *Schema) Migrate(logger *zap.Logger, db *Database, vacuumState int, enable bool) error {
	if len(s.Migrations) == 0 {
		return nil
	}
	before, err := version(db)
	if err != nil {
		return err
	}
	after := 0
	if len(s.Migrations) > 0 {
		after = s.Migrations.Version()
	}
	if before > after {
		logger.Error("database version is newer than expected - downgrade is not supported",
			zap.Int("current version", before),
			zap.Int("target version", after),
		)
		return fmt.Errorf("%w: %d > %d", ErrTooNew, before, after)
	}

	if before == after {
		return nil
	}

	if !enable {
		logger.Error("database version is too old",
			zap.Int("current version", before),
			zap.Int("target version", after),
		)
		return fmt.Errorf("%w: %d < %d", ErrOldSchema, before, after)
	}

	logger.Info("running migrations",
		zap.Int("current version", before),
		zap.Int("target version", after),
	)
	for i, m := range s.Migrations {
		if m.Order() <= before {
			continue
		}
		if err := db.WithTx(context.Background(), func(tx *Tx) error {
			if _, ok := s.skipMigration[m.Order()]; !ok {
				if err := m.Apply(tx); err != nil {
					for j := i; j >= 0 && s.Migrations[j].Order() > before; j-- {
						if e := s.Migrations[j].Rollback(); e != nil {
							err = errors.Join(err, fmt.Errorf("rollback %s: %w", m.Name(), e))
							break
						}
					}

					return fmt.Errorf("apply %s: %w", m.Name(), err)
				}
			}
			// version is set intentionally even if actual migration was skipped
			if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d;", m.Order()), nil, nil); err != nil {
				return fmt.Errorf("update user_version to %d: %w", m.Order(), err)
			}
			return nil
		}); err != nil {
			err = errors.Join(err, db.Close())
			return err
		}

		if vacuumState != 0 && before <= vacuumState {
			if err := Vacuum(db); err != nil {
				err = errors.Join(err, db.Close())
				return err
			}
		}
		before = m.Order()
	}
	return nil
}
