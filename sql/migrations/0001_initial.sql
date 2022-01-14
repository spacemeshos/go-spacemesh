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
    signature VARCHAR NOT NULL,
    pubkey VARCHAR NOT NULL,
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