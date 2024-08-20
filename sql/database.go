package sql

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	// ErrNoConnection is returned if pooled connection is not available.
	ErrNoConnection = errors.New("database: no free connection")
	// ErrNotFound is returned if requested record is not found.
	ErrNotFound = errors.New("database: not found")
	// ErrObjectExists is returned if database constraints didn't allow to insert an object.
	ErrObjectExists = errors.New("database: object exists")
	// ErrTooNew is returned if database version is newer than expected.
	ErrTooNew = errors.New("database version is too new")
	// ErrOldSchema is returned when the database version differs from the expected one
	// and migrations are disabled.
	ErrOldSchema = errors.New("old database version")
)

const (
	beginDefault   = "BEGIN;"
	beginImmediate = "BEGIN IMMEDIATE;"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go github.com/spacemeshos/go-spacemesh/sql Executor

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
		enableMigrations: true,
		connections:      16,
		logger:           zap.NewNop(),
		schema:           &Schema{},
		checkSchemaDrift: true,
	}
}

type conf struct {
	enableMigrations bool
	forceFresh       bool
	forceMigrations  bool
	connections      int
	vacuumState      int
	enableLatency    bool
	cache            bool
	cacheSizes       map[QueryCacheKind]int
	logger           *zap.Logger
	schema           *Schema
	allowSchemaDrift bool
	checkSchemaDrift bool
}

// WithConnections overwrites number of pooled connections.
func WithConnections(n int) Opt {
	return func(c *conf) {
		c.connections = n
	}
}

// WithLogger specifies logger for the database.
func WithLogger(logger *zap.Logger) Opt {
	return func(c *conf) {
		c.logger = logger
	}
}

// WithMigrationsDisabled disables migrations for the database.
// The migrations are enabled by default.
func WithMigrationsDisabled() Opt {
	return func(c *conf) {
		c.enableMigrations = false
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

// WithQueryCache enables in-memory caching of results of some queries.
func WithQueryCache(enable bool) Opt {
	return func(c *conf) {
		c.cache = enable
	}
}

// WithQueryCacheSizes sets query cache sizes for the specified cache kinds.
func WithQueryCacheSizes(sizes map[QueryCacheKind]int) Opt {
	return func(c *conf) {
		if c.cacheSizes == nil {
			c.cacheSizes = maps.Clone(sizes)
		} else {
			maps.Copy(c.cacheSizes, sizes)
		}
	}
}

// WithForceMigrations forces database to run all the migrations instead
// of using a schema snapshot in case of a fresh database.
func WithForceMigrations(force bool) Opt {
	return func(c *conf) {
		c.forceMigrations = true
	}
}

// WithSchema specifies database schema script.
func WithDatabaseSchema(schema *Schema) Opt {
	return func(c *conf) {
		c.schema = schema
	}
}

// WithAllowSchemaDrift prevents Open from failing upon schema drift when schema drift
// checks are enabled. A warning is printed instead.
func WithAllowSchemaDrift(allow bool) Opt {
	return func(c *conf) {
		c.allowSchemaDrift = allow
	}
}

// WithNoCheckSchemaDrift disables schema drift checks.
func WithNoCheckSchemaDrift() Opt {
	return func(c *conf) {
		c.checkSchemaDrift = false
	}
}

func withForceFresh() Opt {
	return func(c *conf) {
		c.forceFresh = true
	}
}

// Opt for configuring database.
type Opt func(c *conf)

// OpenInMemory creates an in-memory database.
func OpenInMemory(opts ...Opt) (*sqliteDatabase, error) {
	opts = append(opts, WithConnections(1), withForceFresh())
	return Open("file::memory:?mode=memory", opts...)
}

// InMemory creates an in-memory database for testing and panics if
// there's an error.
func InMemory(opts ...Opt) *sqliteDatabase {
	db, err := OpenInMemory(opts...)
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
func Open(uri string, opts ...Opt) (*sqliteDatabase, error) {
	config := defaultConf()
	for _, opt := range opts {
		opt(config)
	}
	logger := config.logger.With(zap.String("uri", uri))
	var flags sqlite.OpenFlags
	if !config.forceFresh {
		flags = sqlite.SQLITE_OPEN_READWRITE |
			sqlite.SQLITE_OPEN_WAL |
			sqlite.SQLITE_OPEN_URI |
			sqlite.SQLITE_OPEN_NOMUTEX
	}
	freshDB := config.forceFresh
	pool, err := sqlitex.Open(uri, flags, config.connections)
	if err != nil {
		if config.forceFresh || sqlite.ErrCode(err) != sqlite.SQLITE_CANTOPEN {
			return nil, fmt.Errorf("open db %s: %w", uri, err)
		}
		flags |= sqlite.SQLITE_OPEN_CREATE
		freshDB = true
		pool, err = sqlitex.Open(uri, flags, config.connections)
		if err != nil {
			return nil, fmt.Errorf("create db %s: %w", uri, err)
		}
	}
	db := &sqliteDatabase{pool: pool}
	if config.enableLatency {
		db.latency = newQueryLatency()
	}
	if freshDB && !config.forceMigrations {
		if err := config.schema.Apply(db); err != nil {
			return nil, errors.Join(
				fmt.Errorf("error running schema script: %w", err),
				db.Close())
		}
	} else {
		before, after, err := config.schema.CheckDBVersion(logger, db)
		switch {
		case err != nil:
			return nil, errors.Join(err, db.Close())
		case before != after && config.enableMigrations:
			logger.Info("running migrations",
				zap.Int("current version", before),
				zap.Int("target version", after),
			)
			if err := config.schema.Migrate(
				logger, db, before, config.vacuumState,
			); err != nil {
				return nil, errors.Join(err, db.Close())
			}
		case before != after:
			logger.Error("database version is too old",
				zap.Int("current version", before),
				zap.Int("target version", after),
			)
			return nil, errors.Join(
				fmt.Errorf("%w: %d < %d", ErrOldSchema, before, after),
				db.Close())
		}
	}

	if config.checkSchemaDrift {
		loaded, err := LoadDBSchemaScript(db)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("error loading database schema: %w", err),
				db.Close())
		}
		diff := config.schema.Diff(loaded)
		switch {
		case diff == "": // ok
		case config.allowSchemaDrift:
			logger.Warn("database schema drift detected",
				zap.String("uri", uri),
				zap.String("diff", diff),
			)
		default:
			return nil, errors.Join(
				fmt.Errorf("schema drift detected (uri %s):\n%s", uri, diff),
				db.Close())
		}
	}

	if config.cache {
		logger.Debug("using query cache", zap.Any("sizes", config.cacheSizes))
		db.queryCache = &queryCache{cacheSizesByKind: config.cacheSizes}
	}
	db.queryCount.Store(0)
	return db, nil
}

func Version(uri string) (int, error) {
	pool, err := sqlitex.Open(uri, sqlite.SQLITE_OPEN_READONLY, 1)
	if err != nil {
		return 0, fmt.Errorf("open db %s: %w", uri, err)
	}
	db := &sqliteDatabase{pool: pool}
	defer db.Close()
	return version(db)
}

// Database represents a database.
type Database interface {
	Executor
	QueryCache
	Close() error
	QueryCount() int
	QueryCache() QueryCache
	Tx(ctx context.Context) (Transaction, error)
	WithTx(ctx context.Context, exec func(Transaction) error) error
	TxImmediate(ctx context.Context) (Transaction, error)
	WithTxImmediate(ctx context.Context, exec func(Transaction) error) error
}

// Transaction represents a transaction.
type Transaction interface {
	Executor
	Commit() error
	Release() error
}

type sqliteDatabase struct {
	*queryCache
	pool *sqlitex.Pool

	closed   bool
	closeMux sync.Mutex

	latency    *prometheus.HistogramVec
	queryCount atomic.Int64
}

var _ Database = &sqliteDatabase{}

func (db *sqliteDatabase) getConn(ctx context.Context) *sqlite.Conn {
	start := time.Now()
	conn := db.pool.Get(ctx)
	if conn != nil {
		connWaitLatency.Observe(time.Since(start).Seconds())
	}
	return conn
}

func (db *sqliteDatabase) getTx(ctx context.Context, initstmt string) (*sqliteTx, error) {
	conn := db.getConn(ctx)
	if conn == nil {
		return nil, ErrNoConnection
	}
	tx := &sqliteTx{queryCache: db.queryCache, db: db, conn: conn}
	if err := tx.begin(initstmt); err != nil {
		return nil, err
	}
	return tx, nil
}

func (db *sqliteDatabase) withTx(ctx context.Context, initstmt string, exec func(Transaction) error) error {
	tx, err := db.getTx(ctx, initstmt)
	if err != nil {
		return err
	}
	defer tx.Release()
	if err := exec(tx); err != nil {
		tx.queryCache.ClearCache()
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
func (db *sqliteDatabase) Tx(ctx context.Context) (Transaction, error) {
	return db.getTx(ctx, beginDefault)
}

// WithTx will pass initialized deferred transaction to exec callback.
// Will commit only if error is nil.
func (db *sqliteDatabase) WithTx(ctx context.Context, exec func(Transaction) error) error {
	return db.withTx(ctx, beginImmediate, exec)
}

// TxImmediate creates immediate transaction.
//
// IMMEDIATE cause the database connection to start a new write immediately, without waiting
// for a write statement. The BEGIN IMMEDIATE might fail with SQLITE_BUSY if another write
// transaction is already active on another database connection.
func (db *sqliteDatabase) TxImmediate(ctx context.Context) (Transaction, error) {
	return db.getTx(ctx, beginImmediate)
}

// WithTxImmediate will pass initialized immediate transaction to exec callback.
// Will commit only if error is nil.
func (db *sqliteDatabase) WithTxImmediate(
	ctx context.Context,
	exec func(Transaction) error,
) error {
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
func (db *sqliteDatabase) Exec(query string, encoder Encoder, decoder Decoder) (int, error) {
	db.queryCount.Add(1)
	conn := db.getConn(context.Background())
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
func (db *sqliteDatabase) Close() error {
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

// QueryCount returns the number of queries executed, including failed
// queries, but not counting transaction start / commit / rollback.
func (db *sqliteDatabase) QueryCount() int {
	return int(db.queryCount.Load())
}

// Return database's QueryCache.
func (db *sqliteDatabase) QueryCache() QueryCache {
	return db.queryCache
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
			return 0, fmt.Errorf("step %d: %w", rows, mapSqliteError(err))
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

// sqliteTx is wrapper for database transaction.
type sqliteTx struct {
	*queryCache
	db        *sqliteDatabase
	conn      *sqlite.Conn
	committed bool
	err       error
}

func (tx *sqliteTx) begin(initstmt string) error {
	stmt := tx.conn.Prep(initstmt)
	_, err := stmt.Step()
	if err != nil {
		return fmt.Errorf("begin: %w", mapSqliteError(err))
	}
	return nil
}

// Commit transaction.
func (tx *sqliteTx) Commit() error {
	stmt := tx.conn.Prep("COMMIT;")
	_, tx.err = stmt.Step()
	if tx.err != nil {
		return mapSqliteError(tx.err)
	}
	tx.committed = true
	return nil
}

// Release transaction. Every transaction that was created must be released.
func (tx *sqliteTx) Release() error {
	defer tx.db.pool.Put(tx.conn)
	if tx.committed {
		return nil
	}
	stmt := tx.conn.Prep("ROLLBACK")
	_, tx.err = stmt.Step()
	return mapSqliteError(tx.err)
}

// Exec query.
func (tx *sqliteTx) Exec(query string, encoder Encoder, decoder Decoder) (int, error) {
	tx.db.queryCount.Add(1)
	if tx.db.latency != nil {
		start := time.Now()
		defer func() {
			tx.db.latency.WithLabelValues(query).Observe(float64(time.Since(start)))
		}()
	}
	return exec(tx.conn, query, encoder, decoder)
}

func mapSqliteError(err error) error {
	switch sqlite.ErrCode(err) {
	case sqlite.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite.SQLITE_CONSTRAINT_UNIQUE:
		return ErrObjectExists
	case sqlite.SQLITE_INTERRUPT:
		// TODO: we probably should check if there was indeed a context that was
		// canceled
		return context.Canceled
	default:
		return err
	}
}

// Blob represents a binary blob data. It can be reused efficiently
// across multiple data retrieval operations, minimizing reallocations
// of the underlying byte slice.
type Blob struct {
	Bytes []byte
}

// Resize the underlying byte slice to the specified size.
// The returned slice has length equal n, but it might have a larger capacity.
// Warning: it is not guaranteed to keep the old data.
func (b *Blob) Resize(n int) {
	if cap(b.Bytes) < n {
		b.Bytes = make([]byte, n)
	}
	b.Bytes = b.Bytes[:n]
}

func (b *Blob) FromColumn(stmt *Statement, col int) {
	if l := stmt.ColumnLen(col); l != 0 {
		b.Resize(l)
		stmt.ColumnBytes(col, b.Bytes)
	} else {
		b.Resize(0)
	}
}

// GetBlobSizes returns a slice containing the sizes of blobs
// corresponding to the specified ids. For non-existent ids the
// corresponding value is -1.
func GetBlobSizes(db Executor, cmd string, ids [][]byte) (sizes []int, err error) {
	if len(ids) == 0 {
		return nil, nil
	}

	cmd += fmt.Sprintf(" (?%s)", strings.Repeat(",?", len(ids)-1))
	sizes = make([]int, len(ids))
	for n := range sizes {
		sizes[n] = -1
	}
	m := make(map[string]int)
	for n, id := range ids {
		m[string(id)] = n
	}
	id := make([]byte, len(ids[0]))
	if _, err := db.Exec(cmd,
		func(stmt *Statement) {
			for n, id := range ids {
				stmt.BindBytes(n+1, id)
			}
		}, func(stmt *Statement) bool {
			stmt.ColumnBytes(0, id[:])
			n, found := m[string(id)]
			if !found {
				panic("BUG: bad ID retrieved from DB")
			}
			sizes[n] = stmt.ColumnInt(1)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get blob sizes: %w", err)
	}

	return sizes, nil
}

// LoadBlob loads an encoded blob.
func LoadBlob(db Executor, cmd string, id []byte, blob *Blob) error {
	if rows, err := db.Exec(cmd,
		func(stmt *Statement) {
			stmt.BindBytes(1, id)
		}, func(stmt *Statement) bool {
			blob.FromColumn(stmt, 0)
			return true
		}); err != nil {
		return fmt.Errorf("get %v: %w", types.BytesToHash(id), err)
	} else if rows == 0 {
		return fmt.Errorf("%w: object %s", ErrNotFound, hex.EncodeToString(id))
	}
	return nil
}

// IsNull returns true if the specified result column is null.
func IsNull(stmt *Statement, col int) bool {
	return stmt.ColumnType(col) == sqlite.SQLITE_NULL
}

// StateDatabase is a Database used for Spacemesh state.
type StateDatabase interface {
	Database
	IsStateDatabase()
}

// LocalDatabase is a Database used for local node data.
type LocalDatabase interface {
	Database
	IsLocalDatabase()
}
