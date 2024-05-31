DROP INDEX atxs_by_pubkey_by_epoch_desc;
DROP INDEX atxs_by_epoch_by_pubkey;
ALTER TABLE atxs RENAME TO atxs_old;
CREATE TABLE atxs
(
    id                  CHAR(32) PRIMARY KEY,
    epoch               INT NOT NULL,
    effective_num_units INT NOT NULL,
    commitment_atx      CHAR(32),
    nonce               UNSIGNED LONG INT,
    base_tick_height    UNSIGNED LONG INT,
    tick_count          UNSIGNED LONG INT,
    sequence            UNSIGNED LONG INT,
    pubkey              CHAR(32),
    coinbase            CHAR(24),
    atx                 BLOB,
    received            INT NOT NULL
);
INSERT INTO atxs (id, epoch, effective_num_units, commitment_atx, nonce, base_tick_height, tick_count, sequence, pubkey, coinbase, atx, received)
  SELECT id, epoch, effective_num_units, commitment_atx, nonce, base_tick_height, tick_count, sequence, pubkey, coinbase, atx, received
  FROM atxs_old;
CREATE INDEX atxs_by_pubkey_by_epoch_desc ON atxs (pubkey, epoch desc);
CREATE INDEX atxs_by_epoch_by_pubkey ON atxs (epoch, pubkey);
DROP TABLE atxs_old;

DROP INDEX ballots_by_layer_by_pubkey;
DROP INDEX ballots_by_atx_by_layer;
ALTER TABLE ballots RENAME TO ballots_old;
CREATE TABLE ballots
(
    id        CHAR(20) PRIMARY KEY,
    atx       CHAR(32) NOT NULL,
    layer     INT NOT NULL,
    pubkey    VARCHAR,
    ballot    BLOB
);
INSERT INTO ballots (id, atx, layer, pubkey, ballot)
  SELECT id, atx, layer, pubkey, ballot from ballots_old;
CREATE INDEX ballots_by_layer_by_pubkey ON ballots (layer asc, pubkey);
CREATE INDEX ballots_by_atx_by_layer ON ballots (atx, layer asc);
DROP TABLE ballots_old;

DROP INDEX blocks_by_layer;
ALTER TABLE blocks RENAME TO blocks_old;
CREATE TABLE blocks
(
    id       CHAR(20) PRIMARY KEY,
    layer    INT NOT NULL,
    validity SMALL INT,
    block    BLOB
);
INSERT INTO blocks (id, layer, validity, block)
  SELECT id, layer, validity, block FROM blocks_old;
CREATE INDEX blocks_by_layer ON blocks (layer, id asc);
DROP TABLE blocks_old;

DROP INDEX poets_by_service_id_by_round_id;
ALTER TABLE poets RENAME TO poets_old;
CREATE TABLE poets
(
    ref        VARCHAR PRIMARY KEY,
    poet       BLOB,
    service_id VARCHAR,
    round_id   VARCHAR
);
INSERT INTO poets (ref, poet, service_id, round_id)
  SELECT ref, poet, service_id, round_id FROM poets_old;
CREATE INDEX poets_by_service_id_by_round_id ON poets (service_id, round_id);
DROP TABLE poets_old;

ALTER TABLE certificates RENAME TO certificates_old;
CREATE TABLE certificates
(
    layer INT NOT NULL,
    block VARCHAR NOT NULL,
    cert  BLOB,
    valid bool NOT NULL,
    PRIMARY KEY (layer, block)
);
INSERT INTO certificates (layer, block, cert, valid)
  SELECT layer, block, cert, valid FROM certificates_old;
DROP TABLE certificates_old;

DROP INDEX proposals_by_layer;
ALTER TABLE proposals RENAME TO proposals_old;
CREATE TABLE proposals
(
    id         CHAR(20) PRIMARY KEY,
    ballot_id  CHAR(20),
    layer      INT NOT NULL,
    tx_ids     BLOB,
    mesh_hash  CHAR(32),
    signature  VARCHAR,
    proposal   BLOB
);
INSERT INTO proposals (id, ballot_id, layer, tx_ids, mesh_hash, signature, proposal)
  SELECT id, ballot_id, layer, tx_ids, mesh_hash, signature, proposal FROM proposals_old;
CREATE INDEX proposals_by_layer ON proposals (layer);
DROP TABLE proposals_old;
