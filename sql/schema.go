package sql

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
)

const (
	SchemaPath        = "schema/schema.sql"
	UpdatedSchemaPath = "schema/schema.sql.updated"
)

// LoadDBSchemaScript retrieves the database schema as text.
func LoadDBSchemaScript(db Executor) (string, error) {
	var (
		err error
		sb  strings.Builder
	)
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
			fmt.Fprintln(&sb, st.ColumnText(1))
			return true
		}); err != nil {
		return "", fmt.Errorf("error retrieving DB schema: %w", err)
	}
	// On Windows, the result contains extra carriage returns
	return strings.ReplaceAll(sb.String(), "\r", ""), nil
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
	opt := cmp.Comparer(func(x, y string) bool {
		return strings.Join(strings.Fields(x), "") == strings.Join(strings.Fields(y), "")
	})
	return cmp.Diff(s.Script, actualScript, opt)
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
func (s *Schema) Apply(db Database) error {
	return db.WithTx(context.Background(), func(tx Transaction) error {
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

func (s *Schema) CheckDBVersion(logger *zap.Logger, db Database) (before, after int, err error) {
	if len(s.Migrations) == 0 {
		return 0, 0, nil
	}
	before, err = version(db)
	if err != nil {
		return 0, 0, err
	}
	after = s.Migrations.Version()
	if before > after {
		logger.Error("database version is newer than expected - downgrade is not supported",
			zap.Int("current version", before),
			zap.Int("target version", after),
		)
		return before, after, fmt.Errorf("%w: %d > %d", ErrTooNew, before, after)
	}

	return before, after, nil
}

func (s *Schema) setVersion(db Executor, version int) error {
	// binding values in pragma statement is not allowed
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", version), nil, nil); err != nil {
		return fmt.Errorf("update user_version to %d: %w", version, err)
	}
	return nil
}

// Migrate performs database migration. In case if migrations are disabled, the database
// version is checked but no migrations are run, and if the database is too old and
// migrations are disabled, an error is returned.
func (s *Schema) Migrate(logger *zap.Logger, db Database, before, vacuumState int) error {
	if logger.Core().Enabled(zap.DebugLevel) {
		db.Intercept("logQueries", logQueryInterceptor(logger))
		defer db.RemoveInterceptor("logQueries")
	}
	for i, m := range s.Migrations {
		if m.Order() <= before {
			continue
		}
		if err := db.WithTx(context.Background(), func(tx Transaction) error {
			if _, ok := s.skipMigration[m.Order()]; !ok {
				if err := m.Apply(tx, logger); err != nil {
					for j := i; j >= 0 && s.Migrations[j].Order() > before; j-- {
						if e := s.Migrations[j].Rollback(); e != nil {
							err = errors.Join(err, fmt.Errorf("rollback %s: %w", m.Name(), e))
							break
						}
					}

					return fmt.Errorf("apply %s: %w", m.Name(), err)
				}
			}
			if err := s.setVersion(tx, m.Order()); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}

		if vacuumState != 0 && before <= vacuumState {
			if err := Vacuum(db); err != nil {
				return err
			}
		}
		before = m.Order()
	}
	return nil
}

// MigrateTempDB performs database migration on the temporary database.
// It doesn't use transactions and the temporary database should be considered
// invalid and discarded if it fails.
// The database is switched into synchronous mode with WAL journal enabled and
// synced after the migrations are completed before setting the database version,
// which triggers file sync.
func (s *Schema) MigrateTempDB(logger *zap.Logger, db Database, before int) error {
	if logger.Core().Enabled(zap.DebugLevel) {
		db.Intercept("logQueries", logQueryInterceptor(logger))
		defer db.RemoveInterceptor("logQueries")
	}
	v := before
	for _, m := range s.Migrations {
		if m.Order() <= v {
			continue
		}

		if _, ok := s.skipMigration[m.Order()]; !ok {
			if err := m.Apply(db, logger); err != nil {
				return fmt.Errorf("apply %s: %w", m.Name(), err)
			}
		}

		// We don't set the version here as if any migration fails,
		// the temporary database is considered invalid and should be discarded.
		v = m.Order()
	}

	logger.Info("syncing temporary database")

	// Enable synchronous mode and WAL journal to ensure the database is synced
	if _, err := db.Exec("PRAGMA journal_mode=WAL", nil, nil); err != nil {
		return fmt.Errorf("PRAGMA journal_mode=WAL: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous=FULL", nil, nil); err != nil {
		return fmt.Errorf("PRAGMA journal_mode=OFF: %w", err)
	}

	// This should trigger file sync
	if err := s.setVersion(db, v); err != nil {
		return err
	}

	return nil
}

// SchemaGenOpt represents a schema generator option.
type SchemaGenOpt func(g *SchemaGen)

func withDefaultOut(w io.Writer) SchemaGenOpt {
	return func(g *SchemaGen) {
		g.defaultOut = w
	}
}

// SchemaGen generates database schema files.
type SchemaGen struct {
	logger     *zap.Logger
	schema     *Schema
	defaultOut io.Writer
}

// NewSchemaGen creates a new SchemaGen instance.
func NewSchemaGen(logger *zap.Logger, schema *Schema, opts ...SchemaGenOpt) *SchemaGen {
	g := &SchemaGen{logger: logger, schema: schema, defaultOut: os.Stdout}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Generate generates database schema and writes it to the specified file.
// If an empty string is specified as outputFile, os.Stdout is used for output.
func (g *SchemaGen) Generate(outputFile string) error {
	db, err := OpenInMemory(
		WithLogger(g.logger),
		WithDatabaseSchema(g.schema),
		WithForceMigrations(true),
		WithNoCheckSchemaDrift())
	if err != nil {
		return fmt.Errorf("error opening in-memory db: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			g.logger.Error("error closing in-memory db: %w", zap.Error(err))
		}
	}()
	loadedScript, err := LoadDBSchemaScript(db)
	if err != nil {
		return fmt.Errorf("error loading DB schema script: %w", err)
	}
	if outputFile == "" {
		if _, err := io.WriteString(g.defaultOut, loadedScript); err != nil {
			return fmt.Errorf("error writing schema file: %w", err)
		}
	} else if err := os.WriteFile(outputFile, []byte(loadedScript), 0o777); err != nil {
		return fmt.Errorf("error writing schema file %q: %w", outputFile, err)
	}
	return nil
}

func logQueryInterceptor(logger *zap.Logger) Interceptor {
	return func(query string) error {
		query = strings.TrimSpace(query)
		if p := strings.Index(query, "\n"); p >= 0 {
			query = query[:p]
		}
		logger.Debug("executing query", zap.String("query", query))
		return nil
	}
}
