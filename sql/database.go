package sql

import (
	"context"
	"errors"
	"fmt"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
	"github.com/spacemeshos/go-spacemesh/database"
)

var (
	// ErrNoConnection is returned if pooled connection is not available.
	ErrNoConnection = errors.New("database: no free connection")
	// ErrNotFound is returned if requested record is not found.
	// TODO(dshulyak) is an alias to datatabase.ErrNotFound until full transition is implemented
	ErrNotFound = database.ErrNotFound
	// ErrObjectExists is returned if database contraints didn't allow to insert an object.
	ErrObjectExists = errors.New("database: object exists")
)

// Executor is an interface for executing raw statement.
type Executor interface {
	Exec(string, Encoder, Decoder) (int, error)
}

// Statement is an sqlite statement.
type Statement = sqlite.Stmt

// Encoder for parameters.
// Both positional parameters:
// select block from blocks where id = ?1;
//
// and named parameters are supported:
// select blocks from blocks where id = @id;
//
// For complete information see https://www.sqlite.org/c3ref/bind_blob.html.
type Encoder func(*Statement)

// Decoder for sqlite rows.
type Decoder func(*Statement) bool

func defaultConf() *conf {
	return &conf{
		connections: 16,
		migrations:  embeddedMigrations,
	}
}

type conf struct {
	flags       sqlite.OpenFlags
	connections int
	migrations  Migrations
}

// WithConnections overwrites number of pooled connections.
func WithConnections(n int) Opt {
	return func(c *conf) {
		c.connections = n
	}
}

// WithMigrations overwrites embedded migrations.
func WithMigrations(migrations Migrations) Opt {
	return func(c *conf) {
		c.migrations = migrations
	}
}

// Opt for configuring database.
type Opt func(c *conf)

// InMemory database for testing.
func InMemory() *Database {
	db, err := Open("file::memory:?mode=memory", WithConnections(1))
	if err != nil {
		panic(err)
	}
	return db
}

// Open database with options.
//
// Database is opened in WAL mode and pragma synchronous=normal.
// https://sqlite.org/wal.html
// https://www.sqlite.org/pragma.html#pragma_synchronous
func Open(uri string, opts ...Opt) (*Database, error) {
	config := defaultConf()
	for _, opt := range opts {
		opt(config)
	}
	pool, err := sqlitex.Open(uri, config.flags, config.connections)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", uri, err)
	}
	db := &Database{pool: pool}
	if config.migrations != nil {
		tx, err := db.Tx(context.Background())
		if err != nil {
			return nil, err
		}
		defer tx.Release()
		if err := config.migrations(tx); err != nil {
			return nil, err
		}
		tx.Commit()
	}
	return db, nil
}

// Database is an instance of sqlite database.
type Database struct {
	pool *sqlitex.Pool
}

// Tx creates deferred sqlite transaction.
//
// Deferred transactions are not started until the first statement.
// Transaction may be started in read mode and automatically upgraded to write mode
// after one of the write statements.
//
// https://www.sqlite.org/lang_transaction.html
func (db *Database) Tx(ctx context.Context) (*Tx, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return nil, ErrNoConnection
	}
	tx := &Tx{db: db, conn: conn}
	return tx, tx.begin()
}

// ConcurrentTx creates concurrent deferred sqlite transaction.
//
// By default sqlite will support only single writer, with concurrent it will optimistically support many,
// but the code should expect that transaction may be aborted if another concurrent transaction
// modified page that was read or needs to be modified.
//
// https://sqlite.org/src/doc/begin-concurrent/doc/begin_concurrent.md
func (db *Database) ConcurrentTx(ctx context.Context) (*Tx, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return nil, ErrNoConnection
	}
	tx := &Tx{db: db, conn: conn}
	return tx, tx.beginConcurrent()
}

// Exec statement using one of the connection from the pool.
//
// If you care about atomicity of the operation (for example writing rewards to multiple accounts)
// Tx should be used. Otherwise sqlite will not guarantee that all side-effects of operations are
// applied to the database if machine crashes.
//
// Note that Exec will block until datatabase is closed or statement has finished.
// If application needs to control statement execution lifetime use one of the transaction.
func (db *Database) Exec(query string, encoder Encoder, decoder Decoder) (int, error) {
	conn := db.pool.Get(context.Background())
	if conn == nil {
		return 0, ErrNoConnection
	}
	defer db.pool.Put(conn)
	return exec(conn, query, encoder, decoder)
}

// Close closes all pooled connections.
func (db *Database) Close() error {
	if err := db.pool.Close(); err != nil {
		return fmt.Errorf("close pool %w", err)
	}
	return nil
}

func exec(conn *sqlite.Conn, query string, encoder Encoder, decoder Decoder) (int, error) {
	stmt, err := conn.Prepare(query)
	if err != nil {
		return 0, fmt.Errorf("prepare %s: %w", query, err)
	}
	if encoder != nil {
		encoder(stmt)
	}
	defer stmt.ClearBindings()

	rows := 0
	for {
		row, err := stmt.Step()
		if err != nil {
			code := sqlite.ErrCode(err)
			if code == sqlite.SQLITE_CONSTRAINT_PRIMARYKEY {
				return 0, ErrObjectExists
			}
			return 0, fmt.Errorf("step %d: %w", rows, err)
		}
		if !row {
			return rows, nil
		}
		rows++
		if decoder != nil && !decoder(stmt) {
			return rows, nil
		}
	}
}

// Tx is wrapper for database transaction.
type Tx struct {
	db        *Database
	conn      *sqlite.Conn
	committed bool
	err       error
}

func (tx *Tx) begin() error {
	stmt := tx.conn.Prep("BEGIN;")
	_, err := stmt.Step()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	return nil
}

func (tx *Tx) beginConcurrent() error {
	stmt := tx.conn.Prep("BEGIN CONCURRENT;")
	_, err := stmt.Step()
	if err != nil {
		return fmt.Errorf("begin concurrent: %w", err)
	}
	return nil
}

// Commit transaction.
func (tx *Tx) Commit() error {
	stmt := tx.conn.Prep("COMMIT;")
	_, tx.err = stmt.Step()
	if tx.err != nil {
		return tx.err
	}
	tx.committed = true
	return nil
}

// Release transaction. Every transaction that was created must be released.
func (tx *Tx) Release() error {
	defer tx.db.pool.Put(tx.conn)
	if tx.committed {
		return nil
	}
	stmt := tx.conn.Prep("ROLLBACK")
	_, tx.err = stmt.Step()
	return tx.err
}

// Exec query.
func (tx *Tx) Exec(query string, encoder Encoder, decoder Decoder) (int, error) {
	return exec(tx.conn, query, encoder, decoder)
}
