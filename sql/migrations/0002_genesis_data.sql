CREATE TABLE GenesisConfig (
    genesis_id VARCHAR(20) PRIMARY KEY,
    genesis_time DATETIME,
    extradata VARCHAR(255),
) WITHOUT ROWID;