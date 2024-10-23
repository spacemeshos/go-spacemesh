package sqlstore

import "github.com/spacemeshos/go-spacemesh/sql/expr"

var (
	NewLRU       = newLRU
	IDSFromTable = idsFromTable
)

func (st *SyncedTable) GenSelectAll() expr.Statement      { return st.genSelectAll() }
func (st *SyncedTable) GenCount() expr.Statement          { return st.genCount() }
func (st *SyncedTable) GenSelectMaxRowID() expr.Statement { return st.genSelectMaxRowID() }
func (st *SyncedTable) GenSelectRange() expr.Statement    { return st.genSelectRange() }
func (st *SyncedTable) GenRecentCount() expr.Statement    { return st.genRecentCount() }
func (st *SyncedTable) GenSelectRecent() expr.Statement   { return st.genSelectRecent() }
