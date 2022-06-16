package sql

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/spacemeshos/sqlite"
)

// TxRetryOpt for configuring db transactions.
type TxRetryOpt func(tr *txRetryer)

type txRetryer struct {
	retryNum int
	retryMs  time.Duration
}

func newTxRetryer(opts ...TxRetryOpt) *txRetryer {
	tr := &txRetryer{
		retryMs:  10,
		retryNum: 3,
	}
	for _, opt := range opts {
		opt(tr)
	}
	return tr
}

// Tx is wrapper for database transaction.
type Tx struct {
	db        *Database
	conn      *sqlite.Conn
	committed bool
	err       error
}

// WithRetryBusy number of retry db transactions.
func WithRetryBusy(n int) TxRetryOpt {
	return func(t *txRetryer) {
		t.retryNum = n
	}
}

// WithRetryTimeout timeout for retrying db transactions.
func WithRetryTimeout(milliseconds int) TxRetryOpt {
	return func(t *txRetryer) {
		t.retryMs = time.Duration(milliseconds) * time.Millisecond
	}
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

// WithTx wrap executing function in transaction with retry.
func (db *Database) WithTx(ctx context.Context, exec func(Executor) error, opts ...TxRetryOpt) (err error) {
	retryer := newTxRetryer(opts...)
	for i := 0; i < retryer.retryNum; i++ {
		if err = db.wrapTransaction(ctx, exec, false); err == nil {
			return nil
		}
		sqlError := db.GetSQLiteError(err)
		if sqlError != nil && sqlError.Code != sqlite.SQLITE_BUSY { // exit if something strange received.
			return errors.Wrap(err, "error executing transaction")
		}
		time.Sleep(time.Millisecond * retryer.retryMs)
	}
	return errors.Wrap(err, "error executing transaction")
}

// TxConcurrent starts a concurrent transaction. Locking of db is deferred until commit will be called.
// see for details https://www.sqlite.org/cgi/src/doc/begin-concurrent/doc/begin_concurrent.md.
func (db *Database) TxConcurrent(ctx context.Context) (*Tx, error) {
	conn := db.pool.Get(ctx)
	if conn == nil {
		return nil, ErrNoConnection
	}
	tx := &Tx{db: db, conn: conn}
	return tx, tx.beginConcurrent()
}

// WithTxConcurrent wrap executing function in concurrent transaction with retry.
func (db *Database) WithTxConcurrent(ctx context.Context, exec func(Executor) error, opts ...TxRetryOpt) (err error) {
	retryer := newTxRetryer(opts...)
	for i := 0; i < retryer.retryNum; i++ {
		if err = db.wrapTransaction(ctx, exec, true); err == nil {
			return nil
		}
		sqlError := db.GetSQLiteError(err)
		if sqlError != nil && sqlError.Code != sqlite.SQLITE_BUSY { // exit if something strange received.
			return errors.Wrap(err, "error executing transaction")
		}
		time.Sleep(time.Millisecond * retryer.retryMs)
	}
	return errors.Wrap(err, "error executing transaction")
}

func (db *Database) wrapTransaction(ctx context.Context, exec func(Executor) error, concurrent bool) (err error) {
	var tx *Tx
	if concurrent {
		if tx, err = db.TxConcurrent(ctx); err != nil {
			return errors.Wrap(err, "error start concurrent transaction")
		}
	} else {
		if tx, err = db.Tx(ctx); err != nil {
			return errors.Wrap(err, "error start transaction")
		}
	}
	defer tx.Release()

	if err = exec(tx); err != nil {
		return errors.Wrap(err, "error executing transaction")
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "error committing transaction")
	}
	return nil
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
	return exec(tx.conn, query, encoder, decoder)
}
