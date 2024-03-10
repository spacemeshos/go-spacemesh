DROP INDEX atxs_by_pubkey_by_epoch_desc;
DROP INDEX atxs_by_epoch_by_pubkey;
DROP INDEX atxs_by_epoch_by_pubkey_nonce;
DROP INDEX atxs_by_coinbase;
ALTER TABLE atxs RENAME TO atxs_old;

CREATE TABLE atxs_blobs
(
    id                  CHAR(32) PRIMARY KEY,
    atx                 BLOB
);

CREATE TABLE atxs 
(
    epoch               INT NOT NULL,
    id                  CHAR(32),
    effective_num_units INT NOT NULL,
    commitment_atx      CHAR(32),
    nonce               UNSIGNED LONG INT,
    base_tick_height    UNSIGNED LONG INT,
    tick_count          UNSIGNED LONG INT,
    sequence            UNSIGNED LONG INT,
    pubkey              CHAR(32),
    coinbase            CHAR(24),
    received            INT NOT NULL,
    PRIMARY KEY (epoch, id)
);

INSERT INTO atxs (epoch, id, effective_num_units, commitment_atx, nonce, base_tick_height, tick_count, sequence, pubkey, coinbase, received)
  SELECT epoch, id, effective_num_units, commitment_atx, nonce, base_tick_height, tick_count, sequence, pubkey, coinbase, received
  FROM atxs_old;

INSERT INTO atxs_blobs (id, atx) SELECT id, atx FROM atxs_old;

DROP TABLE atxs_old;

CREATE INDEX atxs_by_pubkey_by_epoch_desc ON atxs (pubkey, epoch desc);
CREATE INDEX atxs_by_epoch_by_pubkey ON atxs (epoch, pubkey);
CREATE INDEX atxs_by_coinbase ON atxs (coinbase);
CREATE INDEX atxs_by_epoch_by_pubkey_nonce ON atxs (pubkey, epoch desc, nonce) WHERE nonce IS NOT NULL;