CREATE TABLE blocks ( 
    id CHAR(20) PRIMARY KEY,
    layer INT NOT NULL,
    validity SMALL INT,
    block    BLOB
);
CREATE INDEX blocks_by_layer ON blocks(layer);

CREATE TABLE ballots ( 
    id CHAR(20) PRIMARY KEY,
    layer INT NOT NULL,
    signature VARCHAR,
    pubkey VARCHAR,
    ballot BLOB
);
CREATE INDEX ballots_by_layer ON ballots(layer);

CREATE TABLE layers (
    id INT PRIMARY KEY,
    hare_output VARCHAR,
    hash CHAR(32),
    aggregated_hash CHAR(32)
) WITHOUT ROWID;

CREATE TABLE mesh_status (
    status SMALL INT PRIMARY KEY,
    layer INT NOT NULL
) WITHOUT ROWID;

CREATE TABLE rewards (
    coinbase CHAR(20),
    smesher VARCHAR NOT NULL,
    layer INT,
    total_reward UNSIGNED LONG INT,
    layer_reward UNSIGNED LONG INT,
    PRIMARY KEY (coinbase, smesher, layer)
);

CREATE INDEX rewards_by_coinbase ON rewards(coinbase, layer);
CREATE INDEX rewards_by_smesher ON rewards(smesher, layer);