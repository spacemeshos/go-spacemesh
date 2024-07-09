-- Changes required to handle merged ATXs

ALTER TABLE atxs ADD COLUMN weight INTEGER;
UPDATE atxs SET weight = effective_num_units * tick_count;

ALTER TABLE identities ADD COLUMN marriage_idx INTEGER;
ALTER TABLE identities ADD COLUMN marriage_target CHAR(32);
ALTER TABLE identities ADD COLUMN marriage_signature CHAR(64);
