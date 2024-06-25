PRAGMA user_version = 20;
CREATE TABLE accounts
(
    address        CHAR(24),
    balance        UNSIGNED LONG INT,
    next_nonce     UNSIGNED LONG INT,
    layer_updated  UNSIGNED LONG INT,
    template       CHAR(24),
    state          BLOB,
    PRIMARY KEY (address, layer_updated DESC)
);
CREATE INDEX accounts_by_layer_updated ON accounts (layer_updated);
CREATE TABLE activesets
(
    id     CHAR(32) PRIMARY KEY,
    active_set    BLOB
, epoch INT DEFAULT 0 NOT NULL) WITHOUT ROWID;
CREATE INDEX activesets_by_epoch ON activesets (epoch asc);
CREATE TABLE atx_blobs
(
    id CHAR(32),
    atx BLOB
, version INTEGER);
CREATE UNIQUE INDEX atx_blobs_id ON atx_blobs (id);
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
CREATE INDEX atxs_by_coinbase ON atxs (coinbase);
CREATE INDEX atxs_by_epoch_by_pubkey ON atxs (epoch, pubkey);
CREATE INDEX atxs_by_epoch_by_pubkey_nonce ON atxs (pubkey, epoch desc, nonce) WHERE nonce IS NOT NULL;
CREATE INDEX atxs_by_epoch_id on atxs (epoch, id);
CREATE INDEX atxs_by_pubkey_by_epoch_desc ON atxs (pubkey, epoch desc);
CREATE UNIQUE INDEX atxs_id ON atxs (id);
CREATE TABLE ballots
(
    id        CHAR(20) PRIMARY KEY,
    atx       CHAR(32) NOT NULL,
    layer     INT NOT NULL,
    pubkey    VARCHAR,
    ballot    BLOB
);
CREATE INDEX ballots_by_atx_by_layer ON ballots (atx, layer asc);
CREATE INDEX ballots_by_layer_by_pubkey ON ballots (layer asc, pubkey);
CREATE TABLE beacons
(
    epoch  INT NOT NULL PRIMARY KEY,
    beacon CHAR(4)
) WITHOUT ROWID;
CREATE TABLE block_transactions
(
    tid     CHAR(32),
    bid     CHAR(20),
    layer   INT NOT NULL,
    PRIMARY KEY (tid, bid)
) WITHOUT ROWID;
CREATE TABLE blocks
(
    id       CHAR(20) PRIMARY KEY,
    layer    INT NOT NULL,
    validity SMALL INT,
    block    BLOB
);
CREATE INDEX blocks_by_layer ON blocks (layer, id asc);
CREATE TABLE certificates
(
    layer INT NOT NULL,
    block VARCHAR NOT NULL,
    cert  BLOB,
    valid bool NOT NULL,
    PRIMARY KEY (layer, block)
);
CREATE TABLE identities
(
    pubkey VARCHAR PRIMARY KEY,
    proof  BLOB
, received INT DEFAULT 0 NOT NULL, marriage_atx CHAR(32)) WITHOUT ROWID;
CREATE TABLE layers
(
    id              INT PRIMARY KEY DESC,
    weak_coin       SMALL INT,
    processed       SMALL INT,
    applied_block   VARCHAR,
    state_hash      CHAR(32),
    aggregated_hash CHAR(32)
) WITHOUT ROWID;
CREATE INDEX layers_by_processed ON layers (processed);
CREATE TABLE poets
(
    ref        VARCHAR PRIMARY KEY,
    poet       BLOB,
    service_id VARCHAR,
    round_id   VARCHAR
);
CREATE INDEX poets_by_service_id_by_round_id ON poets (service_id, round_id);
CREATE TABLE proposal_transactions
(
    tid     CHAR(32),
    pid     CHAR(20),
    layer   INT NOT NULL,
    PRIMARY KEY (tid, pid)
) WITHOUT ROWID;
CREATE TABLE recovery
(
    id INTEGER PRIMARY KEY CHECK (id = 1),
    restore INT NOT NULL
);
CREATE TABLE rewards
(
    pubkey       CHAR(32),
    coinbase     CHAR(24) NOT NULL,
    layer        INT NOT NULL,
    total_reward UNSIGNED LONG INT,
    layer_reward UNSIGNED LONG INT,
    PRIMARY KEY (pubkey, layer)
);
CREATE INDEX rewards_by_coinbase ON rewards (coinbase, layer);
CREATE INDEX rewards_by_layer ON rewards (layer asc);
CREATE TABLE transactions
(
    id          CHAR(32) PRIMARY KEY,
    tx          BLOB,
    header      BLOB,
    result      BLOB,
    layer       INT,
    block       CHAR(20),
    principal   CHAR(24),
    nonce       BLOB,
    timestamp   INT NOT NULL
) WITHOUT ROWID;
CREATE INDEX transaction_by_layer_principal ON transactions (layer asc, principal);
CREATE INDEX transaction_by_principal_nonce ON transactions (principal, nonce);
CREATE TABLE transactions_results_addresses
(
    address CHAR(24),
    tid     CHAR(32),
    PRIMARY KEY (tid, address)
) WITHOUT ROWID;
CREATE INDEX transactions_results_addresses_by_address ON transactions_results_addresses(address);
