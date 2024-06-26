-- Table showing the exact number of PoST units commited by smesher in given ATX.
-- TODO(poszu): Migrate data for existing ATXs (require decoding blobs to be correct).
--              Alternatively, we could take the effective numUnits from `atxs` table,
--              which would be faster but it could cause temporary harm for ATXs growing in size.
CREATE TABLE posts (
    atxid  CHAR(32) NOT NULL,
    units  INT NOT NULL,
    pubkey CHAR(32) NOT NULL,
    UNIQUE (atxid, pubkey)
);

CREATE INDEX posts_by_atxid_by_pubkey ON posts (atxid, pubkey);
