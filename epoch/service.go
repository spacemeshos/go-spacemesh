package epoch

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type EpochService struct {
	db sql.Executor
}

func NewEpochService(db sql.Executor) *EpochService {
	return &EpochService{db: db}
}

func (e *EpochService) UpdateEpochStatus(epoch types.EpochID, status types.EpochStatus) error {
	_, err := e.db.Exec(
		"INSERT INTO epochs (epoch_id, epoch_status) VALUES (?1, ?2) ON CONFLICT(epoch_id) DO UPDATE SET epoch_status = ?3",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
			stmt.BindInt64(2, int64(status))
			stmt.BindInt64(3, int64(status))
		},
		nil,
	)
	return err
}

func (e *EpochService) GetEpochStatus(epoch []types.EpochID) (map[types.EpochID]types.EpochStatus, error) {
	result := make(map[types.EpochID]types.EpochStatus)
	sqlS := fmt.Sprintf("SELECT epoch_id, epoch_status FROM epochs WHERE epoch_id IN (%s)", sql.GenerateINPlaceholders(0, len(epoch)))
	_, err := e.db.Exec(sqlS, func(stmt *sql.Statement) {
		for i, ep := range epoch {
			stmt.BindInt64(i+1, int64(ep))
		}
	}, func(stmt *sql.Statement) bool {
		epochID := stmt.ColumnInt32(0)
		epochStatus := stmt.ColumnInt32(1)
		result[types.EpochID(epochID)] = types.EpochStatus(epochStatus)
		return true
	})
	return result, err
}
