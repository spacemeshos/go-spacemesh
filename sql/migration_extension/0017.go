package migration_extension

import (
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

// M0017 executes 0017_dedup_ballots.sql, and it also migrates existing data from original unified ballots table.
// This migration requires parsing ballot data, so it is not straighforward to execute just by using sql.
func M0017() *migration0017 {
	return &migration0017{}
}

type migration0017 struct{}

type refTuple struct {
	beacon             types.Beacon
	totalEligibilities uint32
}

func (migration0017) Name() string {
	return "dedup_ballots"
}

func (migration0017) Order() int {
	return 17
}

func (migration0017) Rollback() error {
	return nil
}

func (m *migration0017) Apply(db sql.Executor) error {
	version, err := sql.Version(db)
	if err != nil {
		return fmt.Errorf("get db version: %w", err)
	}
	if version >= m.Order() {
		return nil
	}
	path := "migrations/state/0017_dedup_ballots.sql"
	f, err := sql.MigrationsFS().Open(path)
	if err != nil {
		return fmt.Errorf("read migration `%s` from embedded fs: %w", path, err)
	}
	scanner := sql.SQLScanner(f)
	for scanner.Scan() {
		_, err = db.Exec(scanner.Text(), nil, nil)
		if err != nil {
			return fmt.Errorf("exec migration `%s`: %w", path, err)
		}
	}
	// 5991539 ballots at the start of epoch 17
	// 20 bytes per id, 9 bytes per tuple
	// at most it should be around 200MB with map ampflication
	// if every ballot is a reference ballot
	var (
		refCache  = map[types.BallotID]refTuple{}
		firstPass = true
		ierr      error
	)
	iter := func(id types.BallotID, reader io.Reader) bool {
		var ballot types.Ballot
		if _, err := codec.DecodeFrom(reader, &ballot); err != nil {
			panic(err)
		}
		var ref refTuple
		if ballot.EpochData != nil && firstPass {
			// this is a reference to a beacon
			ref = refTuple{
				beacon:             ballot.EpochData.Beacon,
				totalEligibilities: ballot.EpochData.EligibilityCount,
			}
			refCache[id] = ref
		} else if !firstPass && ballot.EpochData == nil {
			ref = refCache[ballot.RefBallot]
		} else {
			// skip non-reference ballots in the first pass
			// and reference ballots in the second pass
			return true
		}

		opinion := types.Opinion{
			Votes: ballot.Votes,
			Hash:  ballot.OpinionHash,
		}
		ballotTuple := ballots.BallotTuple{
			Layer:              ballot.Layer,
			ID:                 id,
			ATX:                ballot.AtxID,
			Node:               ballot.SmesherID,
			Eligibilities:      uint32(len(ballot.EligibilityProofs)),
			TotalEligibilities: ref.totalEligibilities,
			Beacon:             ref.beacon,
			Opinion:            opinion.Hash,
		}
		if err := ballots.AddMinimalOpinion(db, ballot.Layer, opinion.Hash, opinion); err != nil {
			ierr = fmt.Errorf("add minimal opinion: %w", err)
			return false
		}
		if err := ballots.AddBallotTuple(db, &ballotTuple); err != nil {
			ierr = fmt.Errorf("add ballot tuple: %w", err)
			return false
		}
		return true
	}
	// in the first pass we populate refCache, we don't have order for blob iteration,
	// and nested selects will make things slower than 2 passes
	if err := ballots.IterateBlobs(db, iter); err != nil || ierr != nil {
		return fmt.Errorf("iterate ballots, first pass: %w, %w", err, ierr)
	}
	firstPass = false
	if err := ballots.IterateBlobs(db, iter); err != nil || ierr != nil {
		return fmt.Errorf("iterate ballots, second pass: %w, %w", err, ierr)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", m.Order()), nil, nil); err != nil {
		return fmt.Errorf("update user_version to %d: %w", m.Order(), err)
	}
	return nil
}
