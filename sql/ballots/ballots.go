package ballots

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
)

func decodeBallot(id types.BallotID, body *bytes.Reader) (*types.Ballot, error) {
	ballot := types.Ballot{}
	if n, err := codec.DecodeFrom(body, &ballot); err != nil {
		if err != io.EOF {
			return nil, fmt.Errorf("decode body of the %s: %w", id, err)
		}
	} else if n == 0 {
		return nil, errors.New("ballot data missing")
	}
	ballot.SetID(id)
	return &ballot, nil
}

// Add ballot to the database.
func Add(db sql.Executor, ballot *types.Ballot) error {
	bytes, err := codec.Encode(ballot)
	if err != nil {
		return fmt.Errorf("encode ballot %s: %w", ballot.ID(), err)
	}
	if _, err := db.Exec(`insert into ballots
		(id, atx, layer, pubkey, ballot)
		values (?1, ?2, ?3, ?4, ?5);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, ballot.ID().Bytes())
			stmt.BindBytes(2, ballot.AtxID.Bytes())
			stmt.BindInt64(3, int64(ballot.Layer))
			stmt.BindBytes(4, ballot.SmesherID.Bytes())
			stmt.BindBytes(5, bytes)
		}, nil); err != nil {
		return fmt.Errorf("insert ballot %s: %w", ballot.ID(), err)
	}
	return nil
}

// Has a ballot in the database.
func Has(db sql.Executor, id types.BallotID) (bool, error) {
	rows, err := db.Exec("select 1 from ballots where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil,
	)
	if err != nil {
		return false, fmt.Errorf("has ballot %s: %w", id, err)
	}
	return rows > 0, nil
}

// GetBlobSizes returns the sizes of the blobs corresponding to ballots with specified
// ids. For non-existent ballots, the corresponding items are set to -1.
func GetBlobSizes(db sql.Executor, ids [][]byte) (sizes []int, err error) {
	return sql.GetBlobSizes(db, "select id, length(ballot) from ballots where id in", ids)
}

// LoadBlob loads ballot as an encoded blob, ready to be sent over the wire.
func LoadBlob(ctx context.Context, db sql.Executor, id []byte, b *sql.Blob) error {
	return sql.LoadBlob(db, "select ballot from ballots where id = ?1", id, b)
}

// Get ballot with id from database.
func Get(db sql.Executor, id types.BallotID) (rst *types.Ballot, err error) {
	var dErr error
	rows, err := db.Exec("select ballot from ballots where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			rst, dErr = decodeBallot(id, stmt.ColumnReader(0))
			return true
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	}
	if dErr != nil {
		return nil, fmt.Errorf("decode ballot %s: %w", id, dErr)
	}
	if rows == 0 {
		return nil, fmt.Errorf("%w: ballot %s", sql.ErrNotFound, id)
	}

	// TODO(mafa): ideally there would be 2 types of ballots - one for persisting that maps to the db
	// and one for the in-memory representation. The in-memory representation would have the information
	// about the maliciousness of the smesher and is fetched via a service that adds malfeasance information
	// either via DB query or from cached data. This is a temporary solution until we have a better way to handle this.
	malicious, err := identities.IsMalicious(db, rst.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("check legacy malfeasance for ballot %s: %w", id, err)
	}
	if malicious {
		rst.SetMalicious()
		return rst, err
	}
	malicious, err = malfeasance.IsMalicious(db, rst.SmesherID)
	if err != nil {
		return nil, fmt.Errorf("check malfeasance for ballot %s: %w", id, err)
	}
	if malicious {
		rst.SetMalicious()
	}
	return rst, nil
}

// Layer returns full body ballot for layer.
// NOTE: this function does not mark the ballot as malicious, if the smesher that published the ATX is!
func Layer(db sql.Executor, lid types.LayerID) (rst []*types.Ballot, err error) {
	if _, err = db.Exec("select id, ballot from ballots where layer = ?1",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid))
		}, func(stmt *sql.Statement) bool {
			id := types.BallotID{}
			stmt.ColumnBytes(0, id[:])
			var ballot *types.Ballot
			ballot, err = decodeBallot(id, stmt.ColumnReader(1))
			if err != nil {
				return false
			}
			rst = append(rst, ballot)
			return true
		}); err != nil {
		return nil, fmt.Errorf("ballots for layer %s: %w", lid, err)
	}
	return rst, err
}

// IDsInLayer returns ballots ids in the layer.
func IDsInLayer(db sql.Executor, lid types.LayerID) (rst []types.BallotID, err error) {
	if _, err := db.Exec("select id from ballots where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BallotID{}
		stmt.ColumnBytes(0, id[:])
		rst = append(rst, id)
		return true
	}); err != nil {
		return nil, fmt.Errorf("ballots for layer %s: %w", lid, err)
	}
	return rst, err
}

// LayerBallotByNodeID returns any ballot by the specified NodeID in a given layer.
func LayerBallotByNodeID(db sql.Executor, lid types.LayerID, nodeID types.NodeID) (*types.Ballot, error) {
	var (
		ballot types.Ballot
		err    error
	)
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid))
		stmt.BindBytes(2, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		var (
			n   int
			bid types.BallotID
		)
		stmt.ColumnBytes(0, bid[:])
		if n, err = codec.DecodeFrom(stmt.ColumnReader(1), &ballot); err != nil {
			if err != io.EOF {
				err = fmt.Errorf("ballot data layer %v, nodeID %v: %w", lid, nodeID, err)
				return false
			}
		} else if n == 0 {
			err = fmt.Errorf("ballot data missing layer %v, nodeID %v", lid, nodeID)
			return false
		}
		ballot.SetID(bid)
		ballot.SmesherID = nodeID
		return true
	}
	if rows, err := db.Exec(`
		select id, ballot from ballots
		where layer = ?1 and pubkey = ?2
		limit 1;`, enc, dec); err != nil {
		return nil, fmt.Errorf("same layer ballot %v: %w", lid, err)
	} else if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return &ballot, err
}

// LatestLayer gets the highest layer with ballots.
func LatestLayer(db sql.Executor) (types.LayerID, error) {
	var lid types.LayerID
	if _, err := db.Exec("select max(layer) from ballots;",
		nil,
		func(stmt *sql.Statement) bool {
			lid = types.LayerID(uint32(stmt.ColumnInt64(0)))
			return true
		}); err != nil {
		return lid, fmt.Errorf("latest layer: %w", err)
	}
	return lid, nil
}

// FirstInEpoch returns the first ballot referencing the specified ATX in the epoch.
// NOTE: it does not mark the ballot as malicious, if the smesher that published the ATX is!
func FirstInEpoch(db sql.Executor, atx types.ATXID, epoch types.EpochID) (*types.Ballot, error) {
	var (
		ballot *types.Ballot
		dErr   error
	)
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.Bytes())
		stmt.BindInt64(2, int64(epoch.FirstLayer()))
		stmt.BindInt64(3, int64((epoch+1).FirstLayer()-1))
	}
	dec := func(stmt *sql.Statement) bool {
		var bid types.BallotID
		stmt.ColumnBytes(0, bid[:])
		ballot, dErr = decodeBallot(bid, stmt.ColumnReader(1))
		if dErr != nil {
			return false
		}
		return ballot.EpochData == nil
	}
	rows, err := db.Exec(`
		select id, ballot from ballots
		where atx = ?1 and layer between ?2 and ?3
		order by layer asc limit 1
	`, enc, dec)
	if err != nil {
		return nil, fmt.Errorf("ballot by atx %s: %w", atx, err)
	}
	if dErr != nil {
		return nil, fmt.Errorf("decode ballot by atx %s: %w", atx, err)
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return ballot, err
}

// LastInEpoch returns the last ballot referencing the specified ATX in the epoch.
// NOTE: it does not mark the ballot as malicious, if the smesher that published the ATX is!
func LastInEpoch(db sql.Executor, atx types.ATXID, epoch types.EpochID) (*types.Ballot, error) {
	var (
		ballot *types.Ballot
		dErr   error
	)
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.Bytes())
		stmt.BindInt64(2, int64(epoch.FirstLayer()))
		stmt.BindInt64(3, int64((epoch+1).FirstLayer()-1))
	}
	dec := func(stmt *sql.Statement) bool {
		var bid types.BallotID
		stmt.ColumnBytes(0, bid[:])
		ballot, dErr = decodeBallot(bid, stmt.ColumnReader(1))
		if dErr != nil {
			return false
		}
		return ballot.EpochData == nil
	}
	rows, err := db.Exec(`
		select id, ballot from ballots
		where atx = ?1 and layer between ?2 and ?3
		order by layer desc limit 1
	`, enc, dec)
	if err != nil {
		return nil, fmt.Errorf("ballot by atx %s: %w", atx, err)
	}
	if dErr != nil {
		return nil, fmt.Errorf("decode ballot by atx %s: %w", atx, err)
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return ballot, err
}

func AllFirstInEpoch(db sql.Executor, epoch types.EpochID) ([]*types.Ballot, error) {
	var (
		err error
		rst []*types.Ballot
	)
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch.FirstLayer()))
		stmt.BindInt64(2, int64((epoch+1).FirstLayer()-1))
	}
	dec := func(stmt *sql.Statement) bool {
		var (
			bid    types.BallotID
			ballot types.Ballot
		)
		stmt.ColumnBytes(0, bid[:])
		if _, err = codec.DecodeFrom(stmt.ColumnReader(1), &ballot); err != nil && err != io.EOF {
			err = fmt.Errorf("decode ballot: %w", err)
			return false
		} else {
			err = nil
		}
		ballot.SetID(bid)
		rst = append(rst, &ballot)
		return true
	}
	if _, err := db.Exec(`
		select id, ballot, min(layer) from ballots where layer between ?1 and ?2
		group by pubkey;`, enc, dec); err != nil {
		return nil, fmt.Errorf("query first ballots in epoch %d: %w", epoch, err)
	}
	if err != nil {
		return nil, err
	}
	return rst, nil
}
