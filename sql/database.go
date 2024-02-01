package sql

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var (
	// ErrNoConnection is returned if pooled connection is not available.
	ErrNoConnection = errors.New("database: no free connection")
	// ErrNotFound is returned if requested record is not found.
	ErrNotFound = errors.New("database: not found")
	// ErrObjectExists is returned if database constraints didn't allow to insert an object.
	ErrObjectExists = errors.New("database: object exists")
)

const (
	beginDefault   = "BEGIN;"
	beginImmediate = "BEGIN IMMEDIATE;"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./database.go

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
	migrations, err := StateMigrations()
	if err != nil {
		panic(err)
	}

	return &conf{
		connections:   16,
		migrations:    migrations,
		skipMigration: map[int]struct{}{},
		logger:        zap.NewNop(),
	}
}

type conf struct {
	flags         sqlite.OpenFlags
	connections   int
	skipMigration map[int]struct{}
	vacuumState   int
	migrations    []Migration
	enableLatency bool
	logger        *zap.Logger
}

// WithConnections overwrites number of pooled connections.
func WithConnections(n int) Opt {
	return func(c *conf) {
		c.connections = n
	}
}

func WithLogger(logger *zap.Logger) Opt {
	return func(c *conf) {
		c.logger = logger
	}
}

// WithMigrations overwrites embedded migrations.
// Migrations are sorted by order before applying.
func WithMigrations(migrations []Migration) Opt {
	return func(c *conf) {
		sort.Slice(migrations, func(i, j int) bool {
			return migrations[i].Order() < migrations[j].Order()
		})
		c.migrations = migrations
	}
}

// WithMigration adds migration to the list of migrations.
// It will overwrite an existing migration with the same order.
func WithMigration(migration Migration) Opt {
	return func(c *conf) {
		for i, m := range c.migrations {
			if m.Order() == migration.Order() {
				c.migrations[i] = migration
				return
			}
			if m.Order() > migration.Order() {
				c.migrations = slices.Insert(c.migrations, i, migration)
				return
			}
		}
		c.migrations = append(c.migrations, migration)
	}
}

// WithSkipMigrations will update database version with executing associated migrations.
// It should be used at your own risk.
func WithSkipMigrations(i ...int) Opt {
	return func(c *conf) {
		for _, index := range i {
			c.skipMigration[index] = struct{}{}
		}
	}
}

// WithVacuumState will execute vacuum if database version before the migration was less or equal to the provided value.
func WithVacuumState(i int) Opt {
	return func(c *conf) {
		c.vacuumState = i
	}
}

// WithLatencyMetering enables metric that track latency for every database query.
// Note that it will be a significant amount of data, and should not be enabled on
// multiple nodes by default.
func WithLatencyMetering(enable bool) Opt {
	return func(c *conf) {
		c.enableLatency = enable
	}
}

// Opt for configuring database.
type Opt func(c *conf)

// InMemory database for testing.
func InMemory(opts ...Opt) *Database {
	opts = append(opts, WithConnections(1))
	db, err := Open("file::memory:?mode=memory", opts...)
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
	if config.enableLatency {
		db.latency = newQueryLatency()
	}
	if config.migrations != nil {
		before, err := version(db)
		if err != nil {
			return nil, err
		}
		after := 0
		if len(config.migrations) > 0 {
			after = config.migrations[len(config.migrations)-1].Order()
		}
		config.logger.Info("running migrations",
			zap.String("uri", uri),
			zap.Int("current version", before),
			zap.Int("target version", after),
		)
		tx, err := db.Tx(context.Background())
		if err != nil {
			return nil, err
		}
		for i, m := range config.migrations {
			if m.Order() <= before {
				continue
			}
			if _, ok := config.skipMigration[m.Order()]; !ok {
				if err := m.Apply(tx); err != nil {
					for j := i; j >= 0 && config.migrations[j].Order() > before; j-- {
						if e := config.migrations[j].Rollback(); e != nil {
							err = errors.Join(err, fmt.Errorf("rollback %s: %w", m.Name(), e))
							break
						}
					}

					tx.Release()
					err = errors.Join(err, db.Close())
					return nil, fmt.Errorf("apply %s: %w", m.Name(), err)
				}
			}
			// version is set intentionally even if actual migration was skipped
			if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d;", m.Order()), nil, nil); err != nil {
				return nil, fmt.Errorf("update user_version to %d: %w", m.Order(), err)
			}
		}
		tx.Commit()
		tx.Release()

		if config.vacuumState != 0 && before <= config.vacuumState {
			if err := Vacuum(db); err != nil {
				return nil, err
			}
		}
	}
	return db, nil
}

// Database is an instance of sqlite database.
type Database struct {
	pool *sqlitex.Pool

	closed   bool
	closeMux sync.Mutex

	latency *prometheus.HistogramVec
}

func (db *Database) getTx(ctx context.Context, initstmt string) (*Tx, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return nil, ErrNoConnection
	}
	tx := &Tx{db: db, conn: conn}
	if err := tx.begin(initstmt); err != nil {
		return nil, err
	}
	return tx, nil
}

func (db *Database) withTx(ctx context.Context, initstmt string, exec func(*Tx) error) error {
	tx, err := db.getTx(ctx, initstmt)
	if err != nil {
		return err
	}
	defer tx.Release()
	if err := exec(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Tx creates deferred sqlite transaction.
//
// Deferred transactions are not started until the first statement.
// Transaction may be started in read mode and automatically upgraded to write mode
// after one of the write statements.
//
// https://www.sqlite.org/lang_transaction.html
func (db *Database) Tx(ctx context.Context) (*Tx, error) {
	return db.getTx(ctx, beginDefault)
}

// WithTx will pass initialized deferred transaction to exec callback.
// Will commit only if error is nil.
func (db *Database) WithTx(ctx context.Context, exec func(*Tx) error) error {
	return db.withTx(ctx, beginImmediate, exec)
}

// TxImmediate creates immediate transaction.
//
// IMMEDIATE cause the database connection to start a new write immediately, without waiting
// for a write statement. The BEGIN IMMEDIATE might fail with SQLITE_BUSY if another write
// transaction is already active on another database connection.
func (db *Database) TxImmediate(ctx context.Context) (*Tx, error) {
	return db.getTx(ctx, beginImmediate)
}

// WithTxImmediate will pass initialized immediate transaction to exec callback.
// Will commit only if error is nil.
func (db *Database) WithTxImmediate(ctx context.Context, exec func(*Tx) error) error {
	return db.withTx(ctx, beginImmediate, exec)
}

// Exec statement using one of the connection from the pool.
//
// If you care about atomicity of the operation (for example writing rewards to multiple accounts)
// Tx should be used. Otherwise sqlite will not guarantee that all side-effects of operations are
// applied to the database if machine crashes.
//
// Note that Exec will block until database is closed or statement has finished.
// If application needs to control statement execution lifetime use one of the transaction.
func (db *Database) Exec(query string, encoder Encoder, decoder Decoder) (int, error) {
	conn := db.pool.Get(context.Background())
	if conn == nil {
		return 0, ErrNoConnection
	}
	defer db.pool.Put(conn)
	if db.latency != nil {
		start := time.Now()
		defer func() {
			db.latency.WithLabelValues(query).Observe(float64(time.Since(start)))
		}()
	}
	return exec(conn, query, encoder, decoder)
}

// Close closes all pooled connections.
func (db *Database) Close() error {
	db.closeMux.Lock()
	defer db.closeMux.Unlock()
	if db.closed {
		return nil
	}
	if err := db.pool.Close(); err != nil {
		return fmt.Errorf("close pool %w", err)
	}
	db.closed = true
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
			if code == sqlite.SQLITE_CONSTRAINT_PRIMARYKEY || code == sqlite.SQLITE_CONSTRAINT_UNIQUE {
				return 0, ErrObjectExists
			}
			return 0, fmt.Errorf("step %d: %w", rows, err)
		}
		if !row {
			return rows, nil
		}
		rows++
		// exhaust iterator
		if decoder == nil {
			continue
		}
		if !decoder(stmt) {
			if err := stmt.Reset(); err != nil {
				return rows, fmt.Errorf("statement reset %w", err)
			}
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

func (tx *Tx) begin(initstmt string) error {
	stmt := tx.conn.Prep(initstmt)
	_, err := stmt.Step()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
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
	if tx.db.latency != nil {
		start := time.Now()
		defer func() {
			tx.db.latency.WithLabelValues(query).Observe(float64(time.Since(start)))
		}()
	}
	return exec(tx.conn, query, encoder, decoder)
}
