--- sqlite doesn't support just adding a NOT NULL constraint, so we create a new column,
--- copy the data, and drop the old column
ALTER TABLE initial_post ADD COLUMN commit_atx_new CHAR(32) NOT NULL;
UPDATE initial_post SET commit_atx_new = commit_atx;
ALTER TABLE initial_post DROP COLUMN commit_atx;
ALTER TABLE initial_post RENAME COLUMN commit_atx_new TO commit_atx;

ALTER TABLE initial_post ADD COLUMN num_units  UNSIGNED INT NOT NULL;
ALTER TABLE initial_post ADD COLUMN vrf_nonce  UNSIGNED LONG INT NOT NULL;
