package config

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Loads the genesis config from DB.
// If the columns are populated, checks that they match the hashed value; errors otherwise
// If there is more than one config stored, errors
func Get(db sql.Executor) (cfg *config.GenesisConfig, err error) {
	if rows, err := db.Exec(`select * from GenesisConfig`, nil, func(s *sql.Statement) bool {

		// Use types.GenesisData.decode
		return true
	}); err != nil {
		return config.DefaultGenesisConfig(), nil
	} else if rows > 1 {
		return nil, fmt.Errorf("conflicting genesis configurations found in DB. Configs found: %d", rows)
		// Error
	} else {
		return cfg, nil
	}
}

func Set(db sql.Executor, cfg *config.GenesisConfig) error {
	return SafeSet(db, cfg)
}

// Sets the genesis config in the database *unless* it is already set
func SafeSet(db sql.Executor, cfg *config.GenesisConfig) error {
	return nil
}

// Checks if the Genesis config correctly matches that in the database
// Specifically, it first validates that the config in db hash correct hash value OR is set to defaults
// If those checks pass, it checks that the cfg passed to this function matches the hash as well
// Assumes, of course, that if ValidateCfg(cfg_from_db) is true and ValidateCfg(cfg_from_config_file) is true,
// then the cfg data in the DB is equivalent to the cfg passed in.
func Validate(db sql.Executor, cfg *config.GenesisConfig) bool {
	return true
}

// Validates that the config values in the db match the hash stored in the db
func ValidateDb(db sql.Executor) (cfg config.GenesisConfig, err error) {
	return *config.DefaultGenesisConfig(), nil
}

// Check if a Genesis config matches a given GenesisId
func ValidateCfg(cfg *config.GenesisConfig, id types.GenesisId)
