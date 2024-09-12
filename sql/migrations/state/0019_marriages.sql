ALTER TABLE identities ADD COLUMN marriage_atx CHAR(32);

CREATE INDEX identities_by_marriage_atx on identities (marriage_atx, pubkey, proof is not NULL);