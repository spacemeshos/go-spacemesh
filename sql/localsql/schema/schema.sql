PRAGMA user_version = 9;
CREATE TABLE atx_sync_requests 
(
    epoch     INT NOT NULL,
    timestamp INT NOT NULL, total INTEGER, downloaded INTEGER,
    PRIMARY KEY (epoch)
) WITHOUT ROWID;
CREATE TABLE atx_sync_state 
(
    epoch     INT NOT NULL,
    id        CHAR(32) NOT NULL,
    requests  INT NOT NULL DEFAULT 0, 
    PRIMARY KEY (epoch, id)
) WITHOUT ROWID;
CREATE TABLE "challenge"
(
    id            CHAR(32) PRIMARY KEY,
    epoch         UNSIGNED INT NOT NULL,
    sequence      UNSIGNED INT NOT NULL,
    prev_atx      CHAR(32) NOT NULL,
    pos_atx       CHAR(32) NOT NULL,
    commit_atx    CHAR(32),
    post_nonce    UNSIGNED INT,
    post_indices  VARCHAR,
    post_pow      UNSIGNED LONG INT
, poet_proof_ref        CHAR(32), poet_proof_membership VARCHAR) WITHOUT ROWID;
CREATE TABLE malfeasance_sync_state
(
  id INT NOT NULL PRIMARY KEY,
  timestamp INT NOT NULL
);
CREATE TABLE nipost
(
    id            CHAR(32) PRIMARY KEY,
    post_nonce    UNSIGNED INT NOT NULL,
    post_indices  VARCHAR NOT NULL,
    post_pow      UNSIGNED LONG INT NOT NULL,

    num_units UNSIGNED INT NOT NULL,
    vrf_nonce UNSIGNED LONG INT NOT NULL,

    poet_proof_membership VARCHAR NOT NULL,
    poet_proof_ref        CHAR(32) NOT NULL,
    labels_per_unit       UNSIGNED INT NOT NULL
) WITHOUT ROWID;
CREATE TABLE poet_certificates
(
    node_id      BLOB NOT NULL,
    certifier_id BLOB NOT NULL,
    certificate  BLOB NOT NULL,
    signature    BLOB NOT NULL
);
CREATE UNIQUE INDEX idx_poet_certificates ON poet_certificates (node_id, certifier_id);
CREATE TABLE poet_registration
(
    id            CHAR(32) NOT NULL,
    hash          CHAR(32) NOT NULL,
    address       VARCHAR NOT NULL,
    round_id      VARCHAR NOT NULL,
    round_end     INT NOT NULL,

    PRIMARY KEY (id, address)
) WITHOUT ROWID;
CREATE TABLE "post"
(
    id            CHAR(32) PRIMARY KEY,
    post_nonce    UNSIGNED INT NOT NULL,
    post_indices  VARCHAR NOT NULL,
    post_pow      UNSIGNED LONG INT NOT NULL,

    num_units     UNSIGNED INT NOT NULL,
    commit_atx    CHAR(32) NOT NULL,
    vrf_nonce     UNSIGNED LONG INT NOT NULL
, challenge BLOB NOT NULL DEFAULT x'0000000000000000000000000000000000000000000000000000000000000000');
CREATE TABLE prepared_activeset
(
    kind          UNSIGNED INT NOT NULL,    
    epoch         UNSIGNED INT NOT NULL,
    id            CHAR(32) NOT NULL,
    weight        UNSIGNED INT NOT NULL,
    data          BLOB NOT NULL,
    PRIMARY KEY (kind, epoch)
) WITHOUT ROWID;
