--- sqlite doesn't support just adding a NOT NULL constraint, so we create a new column,
--- copy the data, drop the old table, and rename the new table to the old name
ALTER TABLE initial_post ADD COLUMN num_units UNSIGNED INT;
ALTER TABLE initial_post ADD COLUMN vrf_nonce UNSIGNED LONG INT;

CREATE TABLE initial_post_new
(
    id            CHAR(32) PRIMARY KEY,
    post_nonce    UNSIGNED INT NOT NULL,
    post_indices  VARCHAR NOT NULL,
    post_pow      UNSIGNED LONG INT NOT NULL,

    num_units     UNSIGNED INT NOT NULL,
    commit_atx    CHAR(32) NOT NULL,
    vrf_nonce     UNSIGNED LONG INT NOT NULL
);

INSERT INTO initial_post_new (
    id, post_nonce, post_indices, post_pow, commit_atx, num_units, vrf_nonce
) SELECT
    id, post_nonce, post_indices, post_pow, commit_atx, num_units, vrf_nonce
FROM initial_post;

DROP TABLE initial_post;
ALTER TABLE initial_post_new RENAME TO initial_post;
