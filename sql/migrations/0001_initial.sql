CREATE TABLE blocks ( 
    id CHAR(20) PRIMARY KEY,
    layer INT,
    validity SMALL INT,
    block    BLOB
);
CREATE INDEX blocks_by_layer ON blocks(layer);

CREATE TABLE ballots ( 
    id CHAR(20) PRIMARY KEY,
    layer INT,
    signature VARCHAR,
    pubkey VARCHAR,
    ballot BLOB
);
CREATE INDEX ballots_by_layer ON ballots(layer);

CREATE TABLE layers (
    id INT PRIMARY KEY,
    hare_output VARCHAR
) WITHOUT ROWID;

CREATE TABLE layers_status (
    status SMALL INT PRIMARY KEY,
    layer INT NOT NULL
) WITHOUT ROWID;