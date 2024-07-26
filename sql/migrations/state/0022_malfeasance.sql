-- adds new table for v2 malfeasance proofs
-- TODO(mafa): in the future add a migration to convert old malfeasance proofs to the new format
--    and then remove proof, received from the old table

CREATE TABLE malfeasance
(
    pubkey      CHAR(32) PRIMARY KEY,
    received    INT NOT NULL, -- unix timestamp

    -- if the following field is not null, then domain and proof are null
    -- check the identity that is referenced for the proof
    married_to  CHAR(32),     -- the pubkey of identity in the marriage set that was proven to be malicious
    FOREIGN KEY (married_to) REFERENCES malfeasance (pubkey), -- ensure that the married_to field is a valid pubkey already in the table

    -- if the following fields are not null, then married_to is null
    domain      INT,          -- domain of the proof
    proof       BLOB          -- proof of the identity to be malicious
) WITHOUT ROWID;
