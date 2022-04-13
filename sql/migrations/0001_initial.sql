CREATE TABLE blocks
(
    id       CHAR(20) PRIMARY KEY,
    layer    INT NOT NULL,
    validity SMALL INT,
    block    BLOB
) WITHOUT ROWID;
CREATE INDEX blocks_by_layer ON blocks (layer, id asc);

CREATE TABLE ballots
(
    id        CHAR(20) PRIMARY KEY,
    layer     INT NOT NULL,
    signature VARCHAR,
    pubkey    VARCHAR,
    ballot    BLOB
) WITHOUT ROWID;
CREATE INDEX ballots_by_layer_by_pubkey ON ballots (layer asc, pubkey);

CREATE TABLE identities
(
    pubkey    VARCHAR PRIMARY KEY,
    malicious bool
) WITHOUT ROWID;

CREATE TABLE layers
(
    id              INT PRIMARY KEY,
    hare_output     VARCHAR,
    applied_block   VARCHAR,
    hash            CHAR(32),
    aggregated_hash CHAR(32)
) WITHOUT ROWID;

CREATE TABLE mesh_status
(
    status SMALL INT PRIMARY KEY,
    layer  INT NOT NULL
) WITHOUT ROWID;

CREATE TABLE rewards
(
    smesher      CHAR(64),
    coinbase     CHAR(20),
    layer        INT NOT NULL,
    total_reward UNSIGNED LONG INT,
    layer_reward UNSIGNED LONG INT,
    PRIMARY KEY (smesher, layer)
) WITHOUT ROWID;
CREATE INDEX rewards_by_coinbase ON rewards (coinbase, layer);

CREATE TABLE transactions
(
    id          CHAR(32) PRIMARY KEY,
    tx          BLOB,
    layer       INT,
    block       CHAR(20),
    origin      CHAR(20),
    destination CHAR(20),
    nonce       UNSIGNED LONG INT,
    timestamp   INT NOT NULL,
    applied     SMALL INT DEFAULT 0
) WITHOUT ROWID;
CREATE INDEX transaction_by_applied ON transactions (applied);
CREATE INDEX transaction_by_origin_nonce ON transactions (origin, nonce);
CREATE INDEX transaction_by_origin ON transactions (origin, layer);
CREATE INDEX transaction_by_destination ON transactions (destination, layer);

CREATE TABLE proposal_transactions
(
    tid     CHAR(32),
    pid     CHAR(32),
    layer   INT NOT NULL,
    PRIMARY KEY (tid, pid)
) WITHOUT ROWID;

CREATE TABLE block_transactions
(
    tid     CHAR(32),
    bid     CHAR(32),
    layer   INT NOT NULL,
    PRIMARY KEY (tid, bid)
) WITHOUT ROWID;

CREATE TABLE beacons
(
    epoch  INT NOT NULL PRIMARY KEY,
    beacon CHAR(4)
) WITHOUT ROWID;

CREATE TABLE atxs
(
    id        CHAR(32) PRIMARY KEY,
    layer     INT NOT NULL,
    epoch     INT NOT NULL,
    smesher   CHAR(64),
    atx       BLOB,
    timestamp INT NOT NULL
) WITHOUT ROWID;
CREATE INDEX atxs_by_smesher_by_epoch_desc ON atxs (smesher, epoch desc);
CREATE INDEX atxs_by_epoch_by_pubkey ON atxs (epoch, smesher);

CREATE TABLE atx_top
(
    id     INT PRIMARY KEY CHECK (id = 1),
    atx_id CHAR(32),
    layer  INT NOT NULL
) WITHOUT ROWID;

CREATE TABLE proposals
(
    id        CHAR(20) PRIMARY KEY,
    ballot_id CHAR(20),
    layer     INT NOT NULL,
    tx_ids    BLOB,
    signature VARCHAR,
    proposal   BLOB
) WITHOUT ROWID;
CREATE INDEX proposals_by_layer ON proposals (layer);

CREATE TABLE poets
(
    ref        VARCHAR PRIMARY KEY,
    poet       BLOB,
    service_id VARCHAR,
    round_id   VARCHAR
) WITHOUT ROWID;

CREATE INDEX poets_by_service_id_by_round_id ON poets (service_id, round_id);
