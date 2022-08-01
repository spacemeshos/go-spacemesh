package blockcerts

import (
    "context"
    "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/log"
    "github.com/spacemeshos/go-spacemesh/sql"
    "github.com/spacemeshos/go-spacemesh/sql/certifiedblocks"
)

func certificateStoringLoop(ctx context.Context,
    completedCerts <-chan types.BlockCertificate, db sql.Executor, logger log.Logger) {

    logger = logger.WithContext(ctx)
    for {
        select {
        case <-ctx.Done():
            return
        case cert := <-completedCerts:
            err := certifiedblocks.Add(db, &cert)
            if err != nil {
                logger.Error("certificate storing loop: %w", err)
            }
        }
    }
}
