CREATE TABLE nipost
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
) WITHOUT ROWID;

CREATE TABLE initial_post
(
    id            CHAR(32) PRIMARY KEY,
    post_nonce    UNSIGNED INT NOT NULL,
    post_indices  VARCHAR NOT NULL,
    post_pow      UNSIGNED LONG INT NOT NULL,

    num_units     UNSIGNED INT NOT NULL,
    commit_atx    CHAR(32) NOT NULL,
    vrf_nonce     UNSIGNED LONG INT NOT NULL
) WITHOUT ROWID;