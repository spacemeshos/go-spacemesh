CREATE TABLE marriages
(
    pubkey          CHAR(32) PRIMARY KEY,   -- pubkey of the identity in the marriage set
    id              INT NOT NULL,           -- id of the marriage set
    marriage_atx    CHAR(32) NOT NULL,      -- marriage atx of the identity (can differ within set because of double marriage)
    marriage_idx    INT NOT NULL,           -- index of the identity in the marriage set
    marriage_target CHAR(32) NOT NULL,      -- target of the marriage certificate (pubkey of identity that signed marriage atx)
    marriage_sig    BLOB NOT NULL,          -- proof of the identity in the marriage set

    UNIQUE (marriage_atx, marriage_idx) -- every index is unique per marriage atx
);

CREATE INDEX marriage_atxs ON marriages (marriage_atx);

-- adds new table for v2 malfeasance proofs
CREATE TABLE malfeasance
(
    pubkey      CHAR(32) PRIMARY KEY, -- pubkey of identity that was proven to be malicious
    marriage_id INT,                  -- id of the marriage set that the identity is part of (or NULL if none)
    received    INT NOT NULL,         -- unix timestamp of when the proof was received/detected
    
    -- values below are set least once per marriage_id
    domain      INT,                  -- domain of the proof
    proof       BLOB                  -- proof of the identity to be malicious
);

-- ensure inserting or updating entries in malfeasance_marriages table references an existing marriage
CREATE TRIGGER malfeasance_check_marriage_id_exists
BEFORE INSERT ON malfeasance
FOR EACH ROW
WHEN NEW.marriage_id IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'marriage_id does not exist in marriages table')
    WHERE NOT EXISTS (SELECT 1 FROM marriages WHERE id = NEW.marriage_id);
END;

CREATE TRIGGER malfeasance_check_marriage_id_update_exists
BEFORE UPDATE ON malfeasance
FOR EACH ROW
WHEN NEW.marriage_id IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'marriage_id does not exist in marriages table')
    WHERE NOT EXISTS (SELECT 1 FROM marriages WHERE id = NEW.marriage_id);
END;

-- ensure an entry in malfeasance table without a marriage_id has a proof
CREATE TRIGGER malfeasance_check_proof_exists
BEFORE INSERT ON malfeasance
FOR EACH ROW
WHEN NEW.marriage_id IS NULL
BEGIN
    SELECT RAISE(ABORT, 'proof must be provided for malfeasance entry without marriage_id')
    WHERE NEW.proof IS NULL OR NEW.domain IS NULL;
END;

ALTER TABLE identities DROP COLUMN marriage_atx;
ALTER TABLE identities DROP COLUMN marriage_idx;
ALTER TABLE identities DROP COLUMN marriage_target;
ALTER TABLE identities DROP COLUMN marriage_signature;

-- TODO(mafa): add a migration to convert old malfeasance proofs to the new format and copy to this table
