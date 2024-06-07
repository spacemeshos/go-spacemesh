-- Changes required to handle merged ATXs

ALTER TABLE atxs ADD COLUMN weight INTEGER;
UPDATE atxs SET weight = effective_num_units * tick_count;

ALTER TABLE identities ADD COLUMN marriage_idx INTEGER;
ALTER TABLE identities ADD COLUMN marriage_target CHAR(32);
ALTER TABLE identities ADD COLUMN marriage_signature CHAR(64);

CREATE TABLE previous_atxs (
    id CHAR(32),
    previous_id CHAR(32),
    FOREIGN KEY (id) REFERENCES atxs(id)
);

INSERT INTO previous_atxs (id, previous_id) SELECT id, prev_id FROM atxs WHERE prev_id IS NOT NULL;

ALTER TABLE atxs DROP COLUMN prev_id;
