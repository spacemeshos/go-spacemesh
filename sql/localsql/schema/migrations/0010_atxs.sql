--- Table for storing blobs of published ATX for regossiping purposes.
CREATE TABLE atx_blobs
(
    id         CHAR(32) PRIMARY KEY,
    pubkey     CHAR(32) NOT NULL,
    epoch      INT NOT NULL,
    atx        BLOB,
    version    INTEGER
);

CREATE UNIQUE INDEX atx_blobs_epoch_pubkey ON atx_blobs (epoch, pubkey);
