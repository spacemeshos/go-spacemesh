package crypto

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinXY(t *testing.T) {
	const MaxUintValue = ^uint(0)
	const MinUintValue = 0
	const MaxIntValue = int(MaxUintValue >> 1)
	const MinIntValue = -MaxIntValue - 1

	const MaxUint32Value = ^uint32(0)
	const MinUint32Value = 0
	const MaxInt32Value = int32(MaxUint32Value >> 1)
	const MinInt32Value = -MaxInt32Value - 1

	const MaxUint64Value = ^uint64(0)
	const MinUint64Value = 0
	const MaxInt64Value = int64(MaxUint64Value >> 1)
	const MinInt64Value = -MaxInt64Value - 1

	const FavouriteInt = 17
	const FavouriteInt32 = int32(17)
	const FavouriteInt64 = int64(17)

	//int
	// biggest vs smallest
	try := MinInt(MaxIntValue, MinIntValue)
	assert.Equal(t, try, MinIntValue, fmt.Sprintf("big vs small should be %d, is %d", MinIntValue, try))
	// smallest vs biggest
	try = MinInt(MinIntValue, MaxIntValue)
	assert.Equal(t, try, MinIntValue, fmt.Sprintf("small vs big should be %d, is %d", MinIntValue, try))
	// biggest -1 vs biggest
	try = MinInt(MaxIntValue-1, MaxIntValue)
	assert.Equal(t, try, MaxIntValue-1, fmt.Sprintf("big vs biggest should be %d, is %d", MaxIntValue-1, try))
	// smallest+1 vs smallest
	try = MinInt(MinIntValue+1, MinIntValue)
	assert.Equal(t, try, MinIntValue, fmt.Sprintf("tiny vs smallest should be %d, is %d", MinIntValue, try))
	// the same
	try = MinInt(FavouriteInt, FavouriteInt)
	assert.Equal(t, try, FavouriteInt, fmt.Sprintf("equal values should be %d, is %d", FavouriteInt, try))

	//int32
	// biggest vs smallest
	try32 := MinInt32(MaxInt32Value, MinInt32Value)
	assert.Equal(t, try32, MinInt32Value, fmt.Sprintf("int32 big vs small should be %d, is %d", MinInt32Value, try32))
	// smallest vs biggest
	try32 = MinInt32(MinInt32Value, MaxInt32Value)
	assert.Equal(t, try32, MinInt32Value, fmt.Sprintf("int32 small vs big should be %d, is %d", MinInt32Value, try32))
	// biggest -1 vs biggest
	try32 = MinInt32(MaxInt32Value-1, MaxInt32Value)
	assert.Equal(t, try32, MaxInt32Value-1, fmt.Sprintf("int32 big vs biggest should be %d, is %d", MaxInt32Value-1, try32))
	// smallest+1 vs smallest
	try32 = MinInt32(MinInt32Value+1, MinInt32Value)
	assert.Equal(t, try32, MinInt32Value, fmt.Sprintf("int32 tiny vs smallest should be %d, is %d", MinInt32Value, try32))
	// the same
	try32 = MinInt32(FavouriteInt32, FavouriteInt32)
	assert.Equal(t, try32, FavouriteInt32, fmt.Sprintf("int32 equal values should be %d, is %d", FavouriteInt32, try32))

	//int64
	// biggest vs smallest
	try64 := MinInt64(MaxInt64Value, MinInt64Value)
	assert.Equal(t, try64, MinInt64Value, fmt.Sprintf("int64 big vs small should be %d, is %d", MinInt64Value, try64))
	// smallest vs biggest
	try64 = MinInt64(MinInt64Value, MaxInt64Value)
	assert.Equal(t, try64, MinInt64Value, fmt.Sprintf("int64 small vs big should be %d, is %d", MinInt64Value, try64))
	// biggest -1 vs biggest
	try64 = MinInt64(MaxInt64Value-1, MaxInt64Value)
	assert.Equal(t, try64, MaxInt64Value-1, fmt.Sprintf("int64 big vs biggest should be %d, is %d", MaxInt64Value-1, try64))
	// smallest+1 vs smallest
	try64 = MinInt64(MinInt64Value+1, MinInt64Value)
	assert.Equal(t, try64, MinInt64Value, fmt.Sprintf("int64 tiny vs smallest should be %d, is %d", MinInt64Value, try64))
	// the same
	try64 = MinInt64(FavouriteInt64, FavouriteInt64)
	assert.Equal(t, try64, FavouriteInt64, fmt.Sprintf("int64 equal values should be %d, is %d", FavouriteInt64, try64))

}
