--- sqlite doesn't support just adding a NOT NULL constraint, so we create a new column,
--- copy the data, drop the old column, and rename the new column to the old name
ALTER TABLE initial_post ADD COLUMN commit_atx_new CHAR(32) NOT NULL;
UPDATE initial_post SET commit_atx_new = commit_atx;
ALTER TABLE initial_post DROP COLUMN commit_atx;
ALTER TABLE initial_post RENAME COLUMN commit_atx_new TO commit_atx;

ALTER TABLE initial_post ADD COLUMN num_units UNSIGNED INT NOT NULL;
ALTER TABLE initial_post ADD COLUMN vrf_nonce UNSIGNED LONG INT NOT NULL;

ALTER TABLE nipost RENAME TO challenge;
ALTER TABLE challenge ADD COLUMN poet_proof_ref        CHAR(32);
ALTER TABLE challenge ADD COLUMN poet_proof_membership VARCHAR;

CREATE TABLE poet_registration
(
    id            CHAR(32) NOT NULL,
    hash          CHAR(32) NOT NULL,
    address       VARCHAR NOT NULL,
    round_id      VARCHAR NOT NULL,
    round_end     INT NOT NULL,

    PRIMARY KEY (id, address)
) WITHOUT ROWID;

CREATE TABLE nipost
(
    id            CHAR(32) PRIMARY KEY,
    post_nonce    UNSIGNED INT NOT NULL,
    post_indices  VARCHAR NOT NULL,
    post_pow      UNSIGNED LONG INT NOT NULL,

    poet_proof_membership VARCHAR NOT NULL,
    poet_proof_ref        CHAR(32) NOT NULL,
    labels_per_unit       UNSIGNED INT NOT NULL
) WITHOUT ROWID;
