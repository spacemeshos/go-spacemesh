package sql

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var embedded embed.FS

type migration struct {
	order   int
	name    string
	content *bufio.Scanner
}

// Migrations is interface for migrations provider.
type Migrations func(Executor) error

func embeddedMigrations(db Executor) error {
	files, err := embedded.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("readdir migrations: %w", err)
	}
	var migrations []migration
	for _, file := range files {
		parts := strings.Split(file.Name(), "_")
		if len(parts) < 1 {
			return fmt.Errorf("invalid migration %s", file.Name())
		}
		order, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration %s: %w", file.Name(), err)
		}
		path := filepath.Join("migrations", file.Name())
		content, err := embedded.ReadFile(path)
		if err != nil {
			return fmt.Errorf("readfile %s: %w", path, err)
		}
		scanner := bufio.NewScanner(bytes.NewBuffer(content))
		scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			if i := bytes.Index(data, []byte(";")); i >= 0 {
				return i + 1, data[0 : i+1], nil
			}
			return 0, nil, nil
		})
		migrations = append(migrations, migration{
			order:   order,
			name:    file.Name(),
			content: scanner,
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].order < migrations[j].order
	})

	var current int

	if err := db.Exec("PRAGMA user_version;", nil, func(stmt *Statement) bool {
		current = stmt.ColumnInt(0)
		return true
	}); err != nil {
		return fmt.Errorf("read user_version %w", err)
	}

	for _, m := range migrations {
		if m.order <= current {
			continue
		}
		for m.content.Scan() {
			if err := db.Exec(m.content.Text(), nil, nil); err != nil {
				return fmt.Errorf("exec %s: %w", m.content.Text(), err)
			}
		}
		// binding values in pragma statement is not allowed
		if err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", m.order), nil, nil); err != nil {
			return fmt.Errorf("update user_version to %d: %w", m.order, err)
		}
	}
	return nil
}
