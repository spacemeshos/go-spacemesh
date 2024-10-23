package expr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpr(t *testing.T) {
	for _, tc := range []struct {
		Expr     Expr
		Expected string
	}{
		{
			Expr:     MustParse("a = ? OR x < 10"),
			Expected: `"a" = ? OR "x" < 10`,
		},
		{
			Expr:     Number("1"),
			Expected: `1`,
		},
		{
			Expr:     CountStar(),
			Expected: `count(*)`,
		},
		{
			Expr:     Op(Ident("x"), EQ, Ident("y")),
			Expected: `"x" = "y"`,
		},
		{
			Expr:     MaybeAnd(Op(Ident("x"), EQ, Ident("y"))),
			Expected: `"x" = "y"`,
		},
		{
			Expr:     MaybeAnd(Op(Ident("x"), EQ, Ident("y")), nil, nil),
			Expected: `"x" = "y"`,
		},
		{
			Expr: MaybeAnd(Op(Ident("x"), EQ, Ident("y")),
				Op(Ident("a"), EQ, Bind())),
			Expected: `"x" = "y" AND "a" = ?`,
		},
		{
			Expr: MaybeAnd(Op(Ident("x"), EQ, Ident("y")),
				nil,
				Op(Ident("a"), EQ, Bind())),
			Expected: `"x" = "y" AND "a" = ?`,
		},
		{
			Expr:     MaybeAnd(),
			Expected: "",
		},
		{
			Expr:     Between(Ident("x"), Ident("y"), Bind()),
			Expected: `"x" BETWEEN "y" AND ?`,
		},
		{
			Expr:     Call("max", Ident("x")),
			Expected: `max("x")`,
		},
		{
			Expr:     MustParse("a.id"),
			Expected: `"a"."id"`,
		},
	} {
		if tc.Expected == "" {
			require.Nil(t, tc.Expr)
		} else {
			require.Equal(t, tc.Expected, tc.Expr.String())
			require.Equal(t, tc.Expected, MustParse(tc.Expected).String())
		}
	}
}

func TestStatement(t *testing.T) {
	for _, tc := range []struct {
		Statement SelectBuilder
		Expected  string
		Columns   []string
	}{
		{
			Statement: Select(Number("1")),
			Expected:  `SELECT 1`,
			Columns:   []string{"1"},
		},
		{
			Statement: Select(Call("max", Ident("n"))).From(TableSource("mytable")),
			Expected:  `SELECT max("n") FROM "mytable"`,
			Columns:   []string{`max("n")`},
		},
		{
			Statement: Select(Ident("id"), Ident("n")).
				From(TableSource("mytable")).
				Where(Op(Ident("n"), GE, Bind())).
				OrderBy(Asc(Ident("n"))).
				Limit(Bind()),
			Expected: `SELECT "id", "n" FROM "mytable" WHERE "n" >= ? ORDER BY "n" LIMIT ?`,
			Columns:  []string{`"id"`, `"n"`},
		},
		{
			Statement: Select(Ident("id")).
				From(TableSource("mytable")).
				OrderBy(Desc(Ident("id"))).
				Limit(Number("10")),
			Expected: `SELECT "id" FROM "mytable" ORDER BY "id" DESC LIMIT 10`,
			Columns:  []string{`"id"`},
		},
		{
			Statement: Select(CountStar()).From(TableSource("mytable")),
			Expected:  `SELECT count(*) FROM "mytable"`,
			Columns:   []string{`count(*)`},
		},
		{
			Statement: SelectBasedOn(
				MustParseStatement("select a.id from a left join b on a.x = b.x")).
				Where(Op(Ident("id"), EQ, Bind())),
			Expected: `SELECT "a"."id" FROM "a" LEFT JOIN "b" ON "a"."x" = "b"."x" WHERE "id" = ?`,
			Columns:  []string{`"a"."id"`},
		},
		{
			Statement: SelectBasedOn(
				MustParseStatement("select a.id from a inner join b on a.x = b.x")).
				Columns(CountStar()).
				Where(Op(Ident("id"), EQ, Bind())),
			Expected: `SELECT count(*) FROM "a" INNER JOIN "b" ON "a"."x" = "b"."x" WHERE "id" = ?`,
			Columns:  []string{`count(*)`},
		},
	} {
		require.Equal(t, tc.Expected, tc.Statement.String())
		st := tc.Statement.Get()
		require.Equal(t, tc.Expected, st.String())
		for n, col := range tc.Columns {
			require.Equal(t, col, ColumnExpr(st, n).String())
		}
		require.Equal(t, tc.Expected, MustParseStatement(tc.Expected).String())
	}
}
