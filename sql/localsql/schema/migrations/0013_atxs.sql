CREATE TABLE published_atxs
(
    id         CHAR(32) PRIMARY KEY,
    pubkey     CHAR(32) NOT NULL,
    poetRef    CHAR(32) NOT NULL,
    epoch      INT NOT NULL,
    atx        BLOB,
    version    INTEGER
);

CREATE UNIQUE INDEX published_atx_by_epoch_pubkey ON atx_blobs (epoch, pubkey);

DROP INDEX atx_blobs_epoch_pubkey;
DROP TABLE atx_blobs;
