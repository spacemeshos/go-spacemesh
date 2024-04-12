package sql

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

//go:embed migrations/**/*.sql
var embedded embed.FS

type sqlMigration struct {
	order   int
	name    string
	content *bufio.Scanner
}

func (m *sqlMigration) Apply(db Executor) error {
	current, err := version(db)
	if err != nil {
		return err
	}

	if m.order <= current {
		return nil
	}
	for m.content.Scan() {
		if _, err := db.Exec(m.content.Text(), nil, nil); err != nil {
			return fmt.Errorf("exec %s: %w", m.content.Text(), err)
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

func StateMigrations() ([]Migration, error) {
	return sqlMigrations("state")
}

func LocalMigrations() ([]Migration, error) {
	return sqlMigrations("local")
}

func sqlMigrations(dbname string) ([]Migration, error) {
	var migrations []Migration
	root := fmt.Sprintf("migrations/%s", dbname)
	err := fs.WalkDir(embedded, root, func(path string, d fs.DirEntry, err error) error {
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
		f, err := embedded.Open(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}
		scanner := bufio.NewScanner(f)
		scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			if i := bytes.Index(data, []byte(";")); i >= 0 {
				return i + 1, data[0 : i+1], nil
			}
			return 0, nil, nil
		})
		migrations = append(migrations, &sqlMigration{
			order:   order,
			name:    d.Name(),
			content: scanner,
		})
		return nil
	})
	return migrations, err
}
