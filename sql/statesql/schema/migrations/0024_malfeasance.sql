CREATE TABLE marriages
(
    id              INT NOT NULL,       -- id of the marriage set
    pubkey          CHAR(32) NOT NULL,  -- pubkey of the identity in the marriage set
    marriage_atx    CHAR(32) NOT NULL,  -- marriage atx of the identity (can differ within set because of double marriage)
    marriage_idx    INT NOT NULL,       -- index of the identity in the marriage set
    marriage_target CHAR(32) NOT NULL,  -- target of the marriage certificate (pubkey of identity that signed marriage atx)
    marriage_sig    BLOB NOT NULL,      -- proof of the identity in the marriage set

    UNIQUE (pubkey),                    -- ensure pubkey is unique
    UNIQUE (marriage_atx, marriage_idx) -- every index is unique per marriage atx
);

-- TODO(mafa): unsure about these indexes, need to check if they are needed/beneficial
CREATE INDEX marriage_id_by_pubkey ON marriages (pubkey, id);
CREATE INDEX marriage_atx_by_pubkey ON marriages (pubkey, marriage_atx);

-- adds new table for v2 malfeasance proofs
CREATE TABLE malfeasance
(
    pubkey      CHAR(32) NOT NULL, -- pubkey of identity that was proven to be malicious
    marriage_id INT,               -- id of the marriage set of this identity, null if not part of a marriage set
    received    INT NOT NULL,      -- unix timestamp of when the proof was received/detected
    
    domain      INT,               -- domain of the proof
    proof       BLOB               -- proof of the identity to be malicious
);

-- ensure inserting or updating entries with a marriage_id have a corresponding entry in the marriages table
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

ALTER TABLE identities DROP COLUMN marriage_atx;
ALTER TABLE identities DROP COLUMN marriage_idx;
ALTER TABLE identities DROP COLUMN marriage_target;
ALTER TABLE identities DROP COLUMN marriage_signature;

-- TODO(mafa): add a migration to convert old malfeasance proofs to the new format and copy to this table
