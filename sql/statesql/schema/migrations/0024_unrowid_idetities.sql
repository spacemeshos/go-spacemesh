ALTER TABLE identities RENAME TO identities_old;
CREATE TABLE identities
(
    pubkey BLOB PRIMARY KEY,
    proof  BLOB,
    marriage_atx CHAR(32)
);

INSERT INTO identities (pubkey, proof, marriage_atx)
  SELECT pubkey, proof, marriage_atx FROM identities_old;

DROP TABLE identities_old;