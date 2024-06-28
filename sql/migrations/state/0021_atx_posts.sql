-- Table showing the exact number of PoST units commited by smesher in given ATX.
CREATE TABLE posts (
    atxid  CHAR(32) NOT NULL,
    pubkey CHAR(32) NOT NULL,
    units  INT NOT NULL,
    UNIQUE (atxid, pubkey)
);

CREATE INDEX posts_by_atxid_by_pubkey ON posts (atxid, pubkey);
