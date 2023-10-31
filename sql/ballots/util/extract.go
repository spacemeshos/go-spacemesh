package util

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

func ExtractActiveSet(db sql.Executor) error {
	latest, err := ballots.LatestLayer(db)
	if err != nil {
		return fmt.Errorf("extract get latest: %w", err)
	}
	extracted := 0
	unique := 0
	log.With().Info("extracting ballots active sets",
		log.Uint32("from", types.EpochID(2).FirstLayer().Uint32()),
		log.Uint32("to", latest.Uint32()),
	)
	for lid := types.EpochID(2).FirstLayer(); lid <= latest; lid++ {
		blts, err := ballots.Layer(db, lid)
		if err != nil {
			return fmt.Errorf("extract layer %d: %w", lid, err)
		}
		for _, b := range blts {
			if b.EpochData == nil {
				continue
			}
			if len(b.ActiveSet) == 0 {
				continue
			}
			if err := activesets.Add(db, b.EpochData.ActiveSetHash, &types.EpochActiveSet{
				Epoch: b.Layer.GetEpoch(),
				Set:   b.ActiveSet,
			}); err != nil && !errors.Is(err, sql.ErrObjectExists) {
				return fmt.Errorf(
					"add active set %s (%s): %w",
					b.ID().String(),
					b.EpochData.ActiveSetHash.ShortString(),
					err,
				)
			} else if err == nil {
				unique++
			}
			b.ActiveSet = nil
			if err := ballots.UpdateBlob(db, b.ID(), codec.MustEncode(b)); err != nil {
				return fmt.Errorf("update ballot %s: %w", b.ID().String(), err)
			}
			extracted++
		}
	}
	log.With().Info("extracted active sets from ballots",
		log.Int("num", extracted),
		log.Int("unique", unique),
	)
	return nil
}
