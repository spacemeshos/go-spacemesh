--- sqlite doesn't support just adding a NOT NULL constraint, so we create a new column,
--- copy the data, drop the old column, and rename the new column to the old name
ALTER TABLE initial_post ADD COLUMN commit_atx_new CHAR(32) NOT NULL;
UPDATE initial_post SET commit_atx_new = commit_atx;
ALTER TABLE initial_post DROP COLUMN commit_atx;
ALTER TABLE initial_post RENAME COLUMN commit_atx_new TO commit_atx;

ALTER TABLE initial_post ADD COLUMN num_units        UNSIGNED INT NOT NULL;
ALTER TABLE initial_post ADD COLUMN vrf_nonce        UNSIGNED LONG INT NOT NULL;

CREATE TABLE poet_certificates
(
    node_id     CHAR(32),
    poet_url    VARCHAR NOT NULL,
    certificate VARCHAR NOT NULL
);