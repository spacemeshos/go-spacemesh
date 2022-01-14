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
    hare_output VARCHAR
) WITHOUT ROWID;

CREATE TABLE mesh_status (
    status SMALL INT PRIMARY KEY,
    layer INT NOT NULL
) WITHOUT ROWID;

CREATE TABLE rewards (
    coinbase char(20),
    smesher char(20),
    layer INT,
    total UNSIGNED LONG INT,
    PRIMARY KEY (coinbase, layer)
);

CREATE INDEX smesher_layer ON rewards(smesher, layer);