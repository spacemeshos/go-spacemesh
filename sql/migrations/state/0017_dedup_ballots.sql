DROP INDEX ballots_by_layer_by_pubkey;
DROP INDEX ballots_by_atx_by_layer;
ALTER TABLE ballots RENAME TO ballots_old;

CREATE TABLE ballot_opinions
(   
    layer     INT NOT NULL,
    opinion   CHAR(32) NOT NULL,
    encoded   BLOB,
    PRIMARY KEY (layer, opinion)
);

CREATE TABLE ballots
(
    layer               INT NOT NULL,
    id                  CHAR(20) NOT NULL,
    atx                 CHAR(32) NOT NULL,
    pubkey              CHAR(32) NOT NULL,
    eligibilities       INT NOT NULL,
    beacon              VARCHAR NOT NULL,
    total_eligibilities INT NOT NULL,
    opinion             CHAR(32) NOT NULL,
    FOREIGN KEY (layer, opinion) REFERENCES ballot_opinions (layer, opinion),
    PRIMARY KEY (layer, id)
);

CREATE INDEX ballots_by_layer_by_pubkey ON ballots (layer, pubkey);
CREATE INDEX ballots_by_atx_by_layer ON ballots (atx, layer);

CREATE TABLE ballot_blobs
(
    id        CHAR(20) PRIMARY KEY,
    ballot    BLOB
);

INSERT INTO ballot_blobs (id, ballot) SELECT id, ballot FROM ballots_old;

DROP TABLE ballots_old;