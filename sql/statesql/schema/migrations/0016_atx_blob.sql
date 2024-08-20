CREATE TABLE atx_blobs
(
    id CHAR(32),
    atx BLOB
);

INSERT INTO atx_blobs (id, atx) SELECT id, atx FROM atxs;

CREATE UNIQUE INDEX atx_blobs_id ON atx_blobs (id);

DROP INDEX atxs_by_pubkey_by_epoch_desc;
DROP INDEX atxs_by_epoch_by_pubkey;
DROP INDEX atxs_by_epoch_id;
DROP INDEX atxs_by_coinbase;
DROP INDEX atxs_by_epoch_by_pubkey_nonce;

ALTER TABLE atxs RENAME TO atxs_old;

CREATE TABLE atxs
(
    id                  CHAR(32),
    prev_id             CHAR(32),
    epoch               INT NOT NULL,
    effective_num_units INT NOT NULL,
    commitment_atx      CHAR(32),
    nonce               UNSIGNED LONG INT,
    base_tick_height    UNSIGNED LONG INT,
    tick_count          UNSIGNED LONG INT,
    sequence            UNSIGNED LONG INT,
    pubkey              CHAR(32),
    coinbase            CHAR(24),
    received            INT NOT NULL,
    validity INTEGER DEFAULT false
);

INSERT INTO atxs (id, epoch, effective_num_units, commitment_atx, nonce, base_tick_height, tick_count, sequence, pubkey, coinbase, received, validity)
  SELECT id, epoch, effective_num_units, commitment_atx, nonce, base_tick_height, tick_count, sequence, pubkey, coinbase, received, validity
  FROM atxs_old;

CREATE UNIQUE INDEX atxs_id ON atxs (id);
CREATE INDEX atxs_by_pubkey_by_epoch_desc ON atxs (pubkey, epoch desc);
CREATE INDEX atxs_by_epoch_by_pubkey ON atxs (epoch, pubkey);
CREATE INDEX atxs_by_epoch_id on atxs (epoch, id);
CREATE INDEX atxs_by_coinbase ON atxs (coinbase);
CREATE INDEX atxs_by_epoch_by_pubkey_nonce ON atxs (pubkey, epoch desc, nonce) WHERE nonce IS NOT NULL;

DROP TABLE atxs_old;
