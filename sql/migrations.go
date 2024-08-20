package sql

import (
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// MigrationList denotes a list of migrations.
type MigrationList []Migration

// AddMigration adds a Migration to the MigrationList, overriding the migration with the
// same order number if it already exists. The function returns updated migration list.
// The state of the original migration list is undefined after calling this function.
func (l MigrationList) AddMigration(migration Migration) MigrationList {
	for i, m := range l {
		if m.Order() == migration.Order() {
			l[i] = migration
			return l
		}
		if m.Order() > migration.Order() {
			l = slices.Insert(l, i, migration)
			return l
		}
	}
	return append(l, migration)
}

// Version returns database version for the specified migration list.
func (l MigrationList) Version() int {
	if len(l) == 0 {
		return 0
	}
	return l[len(l)-1].Order()
}

type sqlMigration struct {
	order   int
	name    string
	content string
}

var sqlCommentRx = regexp.MustCompile(`(?m)--.*$`)

func (m *sqlMigration) Apply(db Executor, logger *zap.Logger) error {
	current, err := version(db)
	if err != nil {
		return err
	}

	if m.order <= current {
		return nil
	}
	// TODO: use more advanced approach to split the SQL script
	// into commands
	for _, cmd := range strings.Split(m.content, ";") {
		cmd = sqlCommentRx.ReplaceAllString(cmd, "")
		if strings.TrimSpace(cmd) != "" {
			if _, err := db.Exec(cmd, nil, nil); err != nil {
				return fmt.Errorf("exec %s: %w", cmd, err)
			}
		}
	}
	// binding values in pragma statement is not allowed
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", m.order), nil, nil); err != nil {
		return fmt.Errorf("update user_version to %d: %w", m.order, err)
	}

	return nil
}

func (m *sqlMigration) Name() string {
	return m.name
}

func (m *sqlMigration) Order() int {
	return m.order
}

func (sqlMigration) Rollback() error {
	// handled by the DB itself
	return nil
}

func version(db Executor) (int, error) {
	var current int
	if _, err := db.Exec("PRAGMA user_version;", nil, func(stmt *Statement) bool {
		current = stmt.ColumnInt(0)
		return true
	}); err != nil {
		return 0, fmt.Errorf("read user_version %w", err)
	}
	return current, nil
}

func LoadSQLMigrations(fsys fs.FS) (MigrationList, error) {
	var migrations MigrationList
	err := fs.WalkDir(fsys, "schema/migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walkdir %s: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		parts := strings.Split(d.Name(), "_")
		if len(parts) < 1 {
			return fmt.Errorf("invalid migration %s", d.Name())
		}
		order, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration %s: %w", d.Name(), err)
		}
		script, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}
		migrations = append(migrations, &sqlMigration{
			order:   order,
			name:    d.Name(),
			content: string(script),
		})
		return nil
	})
	return migrations, err
}
