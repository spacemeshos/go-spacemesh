CREATE TABLE blocks ( 
	id CHAR(20) PRIMARY KEY,
	layer INT,
	hare_output BOOL,
	verified BOOL,
	block    BLOB
);

CREATE INDEX blocks_by_layer ON blocks(layer);
CREATE UNIQUE INDEX blocks_by_hare_output ON blocks(layer, hare_output) WHERE hare_output = 1;
CREATE INDEX blocks_by_verified ON blocks(layer, verified) where verified = 1;

CREATE TABLE ballots ( 
	id CHAR(20) PRIMARY KEY,
	layer INT,
    signature BLOB,
    pubkey BLOB,
	ballot BLOB
);

CREATE INDEX ballots_by_layer ON ballots(layer);