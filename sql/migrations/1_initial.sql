CREATE TABLE blocks ( 
	id CHAR(20) PRIMARY KEY,
	layer INT,
	verified BOOL,
	block    BLOB
);

CREATE INDEX blocks_by_layer ON blocks(layer);
CREATE INDEX blocks_by_verified ON blocks(layer, verified) where verified = 1;

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
    hare_output VARCHAR,
    status INT
) WITHOUT ROWID;

CREATE INDEX layers_by_status ON layers(status);