CREATE TABLE recovery
(
    id INTEGER PRIMARY KEY CHECK (id = 1),
    restore INT NOT NULL
);

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
    atx       CHAR(32) NOT NULL,
    layer     INT NOT NULL,
    pubkey    VARCHAR,
    ballot    BLOB
) WITHOUT ROWID;
CREATE INDEX ballots_by_layer_by_pubkey ON ballots (layer asc, pubkey);

CREATE TABLE identities
(
    pubkey VARCHAR PRIMARY KEY,
    proof  BLOB
) WITHOUT ROWID;

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

CREATE TABLE certificates
(
    layer INT NOT NULL,
    block VARCHAR NOT NULL,
    cert  BLOB,
    valid bool NOT NULL,
    PRIMARY KEY (layer, block)
) WITHOUT ROWID;

CREATE TABLE rewards
(
    coinbase     CHAR(24),
    layer        INT NOT NULL,
    total_reward UNSIGNED LONG INT,
    layer_reward UNSIGNED LONG INT,
    PRIMARY KEY (coinbase, layer)
) WITHOUT ROWID;
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
CREATE INDEX transaction_by_principal_nonce ON transactions (principal, nonce);
CREATE INDEX transaction_by_layer_principal ON transactions (layer asc, principal);

CREATE TABLE transactions_results_addresses
(
    address CHAR(24),
    tid     CHAR(32),
    PRIMARY KEY (tid, address)
) WITHOUT ROWID;
CREATE INDEX transactions_results_addresses_by_address ON transactions_results_addresses(address);

CREATE TABLE proposal_transactions
(
    tid     CHAR(32),
    pid     CHAR(20),
    layer   INT NOT NULL,
    PRIMARY KEY (tid, pid)
) WITHOUT ROWID;

CREATE TABLE block_transactions
(
    tid     CHAR(32),
    bid     CHAR(20),
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
) WITHOUT ROWID;
CREATE INDEX atxs_by_pubkey_by_epoch_desc ON atxs (pubkey, epoch desc);
CREATE INDEX atxs_by_epoch_by_pubkey ON atxs (epoch, pubkey);

CREATE TABLE proposals
(
    id         CHAR(20) PRIMARY KEY,
    ballot_id  CHAR(20),
    layer      INT NOT NULL,
    tx_ids     BLOB,
    mesh_hash  CHAR(32),
    signature  VARCHAR,
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