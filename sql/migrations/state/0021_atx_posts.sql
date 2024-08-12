-- Table showing the PoST commitment by a smesher in given ATX.
-- It shows the exact number of space units committed and the previous ATX id.
CREATE TABLE posts (
    atxid          CHAR(32) NOT NULL,
    pubkey         CHAR(32) NOT NULL,
    prev_atxid     CHAR(32),
    prev_atx_index INT,
    units          INT NOT NULL,
    UNIQUE (atxid, pubkey)
);

CREATE INDEX posts_by_atxid_by_pubkey ON posts (atxid, pubkey, prev_atxid);

ALTER TABLE atxs DROP COLUMN prev_id;
