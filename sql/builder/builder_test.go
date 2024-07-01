package builder

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestFilterFrom_WithSingleFilter(t *testing.T) {
	t.Parallel()
	operations := Operations{
		Filter: []Op{
			{Field: Epoch, Token: Eq, Value: types.EpochID(1)},
		},
	}

	expected := " where epoch = ?1"
	actual := FilterFrom(operations)

	if actual != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFilterFrom_WithMultipleFilters(t *testing.T) {
	t.Parallel()
	operations := Operations{
		Filter: []Op{
			{Field: Epoch, Token: Eq, Value: types.EpochID(1)},
			{Field: Smesher, Token: Eq, Value: []byte("smesher")},
		},
	}

	expected := " where epoch = ?1 and pubkey = ?2"
	actual := FilterFrom(operations)

	if actual != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFilterFrom_WithGroupFilters(t *testing.T) {
	t.Parallel()
	operations := Operations{
		Filter: []Op{
			{
				Group: []Op{
					{Field: Epoch, Token: Eq, Value: types.EpochID(1)},
					{Field: Smesher, Token: Eq, Value: []byte("smesher")},
				},
				GroupOperator: And,
			},
		},
	}

	expected := " where ( epoch = ?1 and pubkey = ?2 )"
	actual := FilterFrom(operations)

	if actual != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFilterFrom_WithInToken(t *testing.T) {
	t.Parallel()
	operations := Operations{
		Filter: []Op{
			{Field: Epoch, Token: In, Value: [][]byte{[]byte("1"), []byte("2")}},
		},
	}

	expected := " where epoch in (?1, ?2)"
	actual := FilterFrom(operations)

	if actual != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFilterFrom_WithModifiers(t *testing.T) {
	t.Parallel()
	operations := Operations{
		Filter: []Op{
			{Field: Epoch, Token: Eq, Value: types.EpochID(1)},
		},
		Modifiers: []Modifier{
			{Key: OrderBy, Value: "epoch"},
			{Key: Limit, Value: 10},
		},
	}

	expected := " where epoch = ?1 order by epoch limit 10"
	actual := FilterFrom(operations)

	if actual != expected {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
