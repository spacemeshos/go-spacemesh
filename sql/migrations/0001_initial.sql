CREATE TABLE blocks
(
    id       CHAR(20) PRIMARY KEY,
    layer    INT NOT NULL,
    validity SMALL INT,
    block    BLOB
);
CREATE INDEX blocks_by_layer ON blocks (layer);

CREATE TABLE ballots
(
    id        CHAR(20) PRIMARY KEY,
    layer     INT NOT NULL,
    signature VARCHAR,
    pubkey    VARCHAR,
    ballot    BLOB
);
CREATE INDEX ballots_by_layer_by_pubkey ON ballots (layer, pubkey);

CREATE TABLE identities
(
    pubkey    VARCHAR PRIMARY KEY,
    malicious bool
);

CREATE TABLE layers
(
    id              INT PRIMARY KEY,
    hare_output     VARCHAR,
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
);

CREATE INDEX rewards_by_coinbase ON rewards (coinbase, layer);

CREATE TABLE transactions
(
    id          CHAR(32) PRIMARY KEY,
    tx          BLOB,
    layer       INT NOT NULL,
    block       CHAR(20),
    origin      CHAR(20),
    destination CHAR(20),
    status      INT
);

CREATE INDEX transaction_by_origin ON transactions (origin, layer);
CREATE INDEX transaction_by_destination ON transactions (destination, layer);

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
);

CREATE INDEX atxs_by_smesher_by_epoch_desc ON atxs (smesher, epoch desc);
CREATE INDEX atxs_by_epoch_by_pubkey ON atxs (epoch, smesher);

CREATE TABLE atx_top
(
    id     INT PRIMARY KEY CHECK (id = 1),
    atx_id CHAR(32),
    layer  INT NOT NULL
);

CREATE TABLE poets
(
    ref        VARCHAR PRIMARY KEY,
    poet       BLOB,
    service_id VARCHAR,
    round_id   VARCHAR
) WITHOUT ROWID;

CREATE INDEX poets_by_service_id_by_round_id ON poets (service_id, round_id);
