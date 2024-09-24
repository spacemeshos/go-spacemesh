ALTER TABLE identities RENAME TO identities_old;
CREATE TABLE identities
(
    pubkey VARCHAR PRIMARY KEY,
    is_malicious BOOLEAN DEFAULT FALSE NOT NULL,
    proof  BLOB,
    marriage_atx CHAR(32),
    received INT DEFAULT 0 NOT NULL, 
    marriage_atx CHAR(32), 
    marriage_idx INTEGER, 
    marriage_target CHAR(32), 
    marriage_signature CHAR(64)
);

INSERT INTO identities (pubkey, is_malicious, proof, received, marriage_atx, marriage_idx, marriage_target, marriage_signature)
  SELECT pubkey, proof IS NOT NULL, proof, received, marriage_atx, marriage_idx, marriage_target, marriage_signature FROM identities_old;

DROP TABLE identities_old;

CREATE INDEX malicious_identities ON identities (pubkey) where is_malicious;
CREATE INDEX identities_marriages ON identities (marriage_atx);