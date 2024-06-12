package sql

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

func TestSchemaGen(t *testing.T) {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))
	fs := fstest.MapFS{
		"schema/migrations/0001_first.sql": &fstest.MapFile{
			Data: []byte("create table foo(id int);"),
		},
		"schema/migrations/0002_second.sql": &fstest.MapFile{
			Data: []byte("create table bar(id int);"),
		},
	}
	migrations, err := LoadSQLMigrations(fs)
	require.NoError(t, err)
	require.Len(t, migrations, 2)
	schema := &Schema{
		Script:     "this should not be run",
		Migrations: migrations,
	}
	var sb strings.Builder
	g := NewSchemaGen(logger, schema, withDefaultOut(&sb))
	tempDir := t.TempDir()
	schemaPath := filepath.Join(tempDir, "schema.sql")
	require.NoError(t, g.Generate(schemaPath))
	contents, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	require.Equal(t,
		"PRAGMA user_version = 2;\nCREATE TABLE bar(id int);\nCREATE TABLE foo(id int);\n",
		string(contents))
	require.NoError(t, g.Generate(""))
	require.Equal(t, string(contents), sb.String())

	require.Equal(t, 0, observedLogs.Len(),
		"expected 0 warning messages in the log (schema drift warnings?)")
}
