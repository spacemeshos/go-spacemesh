// Package expr proviedes a simple SQL expression parser and builder.
// It wraps the rqlite/sql package and provides a more convenient API that contains only
// what's needed for the go-spacemesh codebase.
package expr

import (
	"strings"

	rsql "github.com/rqlite/sql"
)

// SQL operations.
const (
	NE     = rsql.NE     // !=
	EQ     = rsql.EQ     // =
	LE     = rsql.LE     // <=
	LT     = rsql.LT     // <
	GT     = rsql.GT     // >
	GE     = rsql.GE     // >=
	BITAND = rsql.BITAND // &
	BITOR  = rsql.BITOR  // |
	BITNOT = rsql.BITNOT // !
	LSHIFT = rsql.LSHIFT // <<
	RSHIFT = rsql.RSHIFT // >>
	PLUS   = rsql.PLUS   // +
	MINUS  = rsql.MINUS  // -
	STAR   = rsql.STAR   // *
	SLASH  = rsql.SLASH  // /
	REM    = rsql.REM    // %
	CONCAT = rsql.CONCAT // ||
	DOT    = rsql.DOT    // .
	AND    = rsql.AND
	OR     = rsql.OR
	NOT    = rsql.NOT
)

// Expr represents a parsed SQL expression.
type Expr = rsql.Expr

// Statement represents a parsed SQL statement.
type Statement = rsql.Statement

// MustParse parses an SQL expression and panics if there's an error.
func MustParse(s string) rsql.Expr {
	expr, err := rsql.ParseExprString(s)
	if err != nil {
		panic("error parsing SQL expression: " + err.Error())
	}
	return expr
}

// MustParseStatement parses an SQL statement and panics if there's an error.
func MustParseStatement(s string) rsql.Statement {
	st, err := rsql.NewParser(strings.NewReader(s)).ParseStatement()
	if err != nil {
		panic("error parsing SQL statement: " + err.Error())
	}
	return st
}

// MaybeAnd joins together several SQL expressions with AND, ignoring any nil exprs.
// If no non-nil expressions are passed, nil is returned.
// If a single non-nil expression is passed, that single expression is returned.
// Otherwise, the expressions are joined together with ANDs:
// a AND b AND c AND d.
func MaybeAnd(exprs ...Expr) Expr {
	var r Expr
	for _, expr := range exprs {
		switch {
		case expr == nil:
		case r == nil:
			r = expr
		default:
			r = Op(r, AND, expr)
		}
	}
	return r
}

// Ident constructs SQL identifier expression for the identifier with the specified name.
func Ident(name string) *rsql.Ident {
	return &rsql.Ident{Name: name}
}

// Number constructs a number literal.
func Number(value string) *rsql.NumberLit {
	return &rsql.NumberLit{Value: value}
}

// TableSource constructs a Source clause for SELECT statement that corresponds to
// selecting from a single table with the specified name.
func TableSource(name string) rsql.Source {
	return &rsql.QualifiedTableName{Name: Ident(name)}
}

// Op constructs a binary expression such as x + y or x < y.
func Op(x Expr, op rsql.Token, y Expr) Expr {
	return &rsql.BinaryExpr{
		X:  x,
		Op: op,
		Y:  y,
	}
}

// Bind constructs the unnamed bind expression (?).
func Bind() Expr {
	return &rsql.BindExpr{Name: "?"}
}

// Between constructs BETWEEN expression: x BETWEEN a AND b.
func Between(x, a, b Expr) Expr {
	return Op(x, rsql.BETWEEN, &rsql.Range{X: a, Y: b})
}

// Call constructs a call expression with specified arguments such as max(x).
func Call(name string, args ...Expr) Expr {
	return &rsql.Call{Name: Ident(name), Args: args}
}

// CountStar returns a COUNT(*) expression.
func CountStar() Expr {
	return &rsql.Call{Name: Ident("count"), Star: rsql.Pos{Offset: 1}}
}

// Asc constructs an ascending ORDER BY term.
func Asc(expr Expr) *rsql.OrderingTerm {
	return &rsql.OrderingTerm{X: expr}
}

// Desc constructs a descedning ORDER BY term.
func Desc(expr Expr) *rsql.OrderingTerm {
	return &rsql.OrderingTerm{X: expr, Desc: rsql.Pos{Offset: 1}}
}

// SelectBuilder is used to construct a SELECT statement.
type SelectBuilder struct {
	st *rsql.SelectStatement
}

// Select returns a SELECT statement builder.
func Select(columns ...any) SelectBuilder {
	sb := SelectBuilder{st: &rsql.SelectStatement{}}
	return sb.Columns(columns...)
}

// SelectBasedOn returns a SELECT statement builder based on the specified SELECT statement.
// The statement must be parseable, otherwise SelectBasedOn panics.
// The builder methods can be used to alter the statement.
func SelectBasedOn(st Statement) SelectBuilder {
	st = rsql.CloneStatement(st)
	return SelectBuilder{st: st.(*rsql.SelectStatement)}
}

// Get returns the underlying SELECT statement.
func (sb SelectBuilder) Get() *rsql.SelectStatement {
	return sb.st
}

// String returns the underlying SELECT statement as a string.
func (sb SelectBuilder) String() string {
	return sb.st.String()
}

// Columns sets columns in the SELECT statement.
func (sb SelectBuilder) Columns(columns ...any) SelectBuilder {
	sb.st.Columns = make([]*rsql.ResultColumn, len(columns))
	for n, column := range columns {
		switch c := column.(type) {
		case *rsql.ResultColumn:
			sb.st.Columns[n] = c
		case Expr:
			sb.st.Columns[n] = &rsql.ResultColumn{Expr: c}
		default:
			panic("unexpected column type")
		}
	}
	return sb
}

// From adds FROM clause to the SELECT statement.
func (sb SelectBuilder) From(s rsql.Source) SelectBuilder {
	sb.st.Source = s
	return sb
}

// From adds WHERE clause to the SELECT statement.
func (sb SelectBuilder) Where(s Expr) SelectBuilder {
	sb.st.WhereExpr = s
	return sb
}

// From adds ORDER BY clause to the SELECT statement.
func (sb SelectBuilder) OrderBy(terms ...*rsql.OrderingTerm) SelectBuilder {
	sb.st.OrderingTerms = terms
	return sb
}

// From adds LIMIT clause to the SELECT statement.
func (sb SelectBuilder) Limit(limit Expr) SelectBuilder {
	sb.st.LimitExpr = limit
	return sb
}

// ColumnExpr returns nth column expression from the SELECT statement.
func ColumnExpr(st Statement, n int) Expr {
	return st.(*rsql.SelectStatement).Columns[n].Expr
}

// WhereExpr returns WHERE expression from the SELECT statement.
func WhereExpr(st Statement) Expr {
	return st.(*rsql.SelectStatement).WhereExpr
}
