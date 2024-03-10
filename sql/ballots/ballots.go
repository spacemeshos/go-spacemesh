package ballots

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func decodeBallot(id types.BallotID, pubkey, body *bytes.Reader, malicious bool) (*types.Ballot, error) {
	var nodeID types.NodeID
	if n, err := pubkey.Read(nodeID[:]); err != nil {
		if err != io.EOF {
			return nil, fmt.Errorf("copy pubkey: %w", err)
		}
	} else if n != types.NodeIDSize {
		return nil, errors.New("public key data missing")
	}
	ballot := types.Ballot{}
	if n, err := codec.DecodeFrom(body, &ballot); err != nil {
		if err != io.EOF {
			return nil, fmt.Errorf("decode body of the %s: %w", id, err)
		}
	} else if n == 0 {
		return nil, errors.New("ballot data missing")
	}
	ballot.SetID(id)
	ballot.SmesherID = nodeID
	if malicious {
		ballot.SetMalicious()
	}
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

func UpdateBlob(db sql.Executor, bid types.BallotID, blob []byte) error {
	if _, err := db.Exec(`update ballots set ballot = ?2 where id = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, bid.Bytes())
			stmt.BindBytes(2, blob[:])
		}, nil); err != nil {
		return fmt.Errorf("update blob %s: %w", bid.String(), err)
	}
	return nil
}

// Get ballot with id from database.
func Get(db sql.Executor, id types.BallotID) (rst *types.Ballot, err error) {
	if rows, err := db.Exec(`select pubkey, ballot, length(identities.proof)
	from ballots left join identities using(pubkey)
	where id = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			rst, err = decodeBallot(id,
				stmt.ColumnReader(0),
				stmt.ColumnReader(1),
				stmt.ColumnInt(2) > 0,
			)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w ballot %s", sql.ErrNotFound, id)
	}
	return rst, nil
}

// Layer returns full body ballot for layer.
func Layer(db sql.Executor, lid types.LayerID) (rst []*types.Ballot, err error) {
	if _, err = db.Exec(`select id, pubkey, ballot, length(identities.proof)
		from ballots left join identities using(pubkey)
		where layer = ?1;`, func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid))
	}, func(stmt *sql.Statement) bool {
		id := types.BallotID{}
		stmt.ColumnBytes(0, id[:])
		var ballot *types.Ballot
		ballot, err = decodeBallot(id,
			stmt.ColumnReader(1),
			stmt.ColumnReader(2),
			stmt.ColumnInt(3) > 0,
		)
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

// LayerNoMalicious returns full ballot without joining malicious identities.
func LayerNoMalicious(db sql.Executor, lid types.LayerID) (rst []*types.Ballot, err error) {
	var derr error
	if _, err = db.Exec(`select id, ballot from ballots where layer = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid))
		}, func(stmt *sql.Statement) bool {
			id := types.BallotID{}
			stmt.ColumnBytes(0, id[:])
			var ballot types.Ballot
			_, derr := codec.DecodeFrom(stmt.ColumnReader(1), &ballot)
			if derr != nil {
				return false
			}
			ballot.SetID(id)
			rst = append(rst, &ballot)
			return true
		}); err != nil {
		return nil, fmt.Errorf("selecting %d: %w", lid, err)
	} else if derr != nil {
		return nil, fmt.Errorf("decoding %d: %w", lid, err)
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

func FirstInEpoch(db sql.Executor, atx types.ATXID, epoch types.EpochID) (*types.Ballot, error) {
	return inEpoch(db, atx, epoch, "asc")
}

func LastInEpoch(db sql.Executor, atx types.ATXID, epoch types.EpochID) (*types.Ballot, error) {
	return inEpoch(db, atx, epoch, "desc")
}

func inEpoch(db sql.Executor, atx types.ATXID, epoch types.EpochID, order string) (*types.Ballot, error) {
	var (
		bid     types.BallotID
		ballot  types.Ballot
		nodeID  types.NodeID
		rows, n int
		err     error
	)
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.Bytes())
		stmt.BindInt64(2, int64(epoch.FirstLayer()))
		stmt.BindInt64(3, int64((epoch+1).FirstLayer()-1))
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, bid[:])
		stmt.ColumnBytes(1, nodeID[:])
		if n, err = codec.DecodeFrom(stmt.ColumnReader(2), &ballot); err != nil {
			if err != io.EOF {
				err = fmt.Errorf("ballot by atx %s: %w", atx, err)
				return false
			}
		} else if n == 0 {
			err = fmt.Errorf("ballot by atx missing data %s", atx)
			return false
		}
		ballot.SetID(bid)
		ballot.SmesherID = nodeID
		if stmt.ColumnInt(3) > 0 {
			ballot.SetMalicious()
		}
		// only ref ballot has valid EpochData
		if ballot.EpochData != nil {
			return false
		}
		return true
	}
	rows, err = db.Exec(fmt.Sprintf(`
		select id, pubkey, ballot, length(identities.proof) from ballots
	    left join identities using(pubkey)
		where atx = ?1 and layer between ?2 and ?3
		order by layer %s limit 1;`, order), enc, dec)
	if err != nil {
		return nil, fmt.Errorf("ballot by atx %s: %w", atx, err)
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return &ballot, err
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

type BallotTuple struct {
	Layer              types.LayerID
	ID                 types.BallotID
	ATX                types.ATXID
	Node               types.NodeID
	Eligibilities      uint32
	Beacon             types.Beacon
	TotalEligibilities uint32
	Opinion            types.Hash32
}

// AddBallotTuple will insert ballot data to the database.
func AddBallotTuple(db sql.Executor, ballot *BallotTuple) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(ballot.Layer))
		stmt.BindBytes(2, ballot.ID[:])
		stmt.BindBytes(3, ballot.ATX.Bytes())
		stmt.BindBytes(4, ballot.Node[:])
		stmt.BindInt64(5, int64(ballot.Eligibilities))
		stmt.BindBytes(6, ballot.Beacon.Bytes())
		stmt.BindInt64(7, int64(ballot.TotalEligibilities))
		stmt.BindBytes(8, ballot.Opinion.Bytes())
	}
	if _, err := db.Exec(`
		insert into ballots(layer, id, atx, pubkey, eligibilities, beacon, total_eligibilities, opinion)
		values(?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8);`, enc, nil); err != nil {
		return fmt.Errorf("add ballot %v: %w", ballot.ID, err)
	}
	return nil
}

// AddMinimalOpinion will insert opinion data to the database.
// If there is another version of encoded opinion we would prefer to keep one that is smaller in size.
func AddMinimalOpinion(db sql.Executor, layer types.LayerID, id types.Hash32, opinion types.Opinion) error {
	encoded := codec.MustEncode(&opinion)
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(layer))
		stmt.BindBytes(2, id[:])
		stmt.BindBytes(3, encoded)
	}
	if _, err := db.Exec(`
		insert into ballot_opinions (layer, opinion, encoded) values (?1, ?2, ?3) 
		on conflict(layer, opinion) where length(encoded) > length(?3) do update set encoded = ?3;`, enc, nil); err != nil {
		return fmt.Errorf("add opinion %v: %w", id, err)
	}
	return nil
}

func IterateBlobs(db sql.Executor, fn func(types.BallotID, io.Reader) bool) error {
	dec := func(stmt *sql.Statement) bool {
		var bid types.BallotID
		stmt.ColumnBytes(0, bid[:])
		return fn(bid, stmt.ColumnReader(1))
	}
	if _, err := db.Exec(`select id, ballot from ballot_blobs;`, nil, dec); err != nil {
		return fmt.Errorf("iterate blobs: %w", err)
	}
	return nil
}

func IterateForTortoise(
	db sql.Executor,
	layer types.LayerID,
	fn func(
		id types.BallotID,
		atxid types.ATXID,
		node types.NodeID,
		eligibilities uint32,
		beacon types.Beacon,
		total uint32,
		opinion types.Opinion,
	) bool,
) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(layer))
	}
	var ierr error
	dec := func(stmt *sql.Statement) bool {
		var (
			id        types.BallotID
			atx       types.ATXID
			node      types.NodeID
			beacon    types.Beacon
			opinionId types.Hash32
			opinion   types.Opinion
		)
		stmt.ColumnBytes(0, id[:])
		stmt.ColumnBytes(1, atx[:])
		stmt.ColumnBytes(2, node[:])
		stmt.ColumnBytes(4, beacon[:])
		stmt.ColumnBytes(6, opinionId[:])
		if _, err := codec.DecodeFrom(stmt.ColumnReader(7), &opinion); err != nil {
			ierr = fmt.Errorf("decode opinion %v/%v: %w", layer, opinionId.ShortString(), err)
			return false
		}
		return fn(id, atx, node, uint32(stmt.ColumnInt64(3)), beacon, uint32(stmt.ColumnInt64(5)), opinion)
	}
	if _, err := db.Exec(`
		select id, atx, pubkey, eligibilities, beacon, total_eligibilities, ballots.opinion, encoded
		from ballots 
		left join ballot_opinions using(layer, opinion)
		where layer = ?1`, enc, dec); err != nil {
		return fmt.Errorf("iterate for tortoise: %w", err)
	}
	return ierr
}
