-- Changes required to handle merged ATXs

ALTER TABLE atxs ADD COLUMN weight INTEGER;
UPDATE atxs SET weight = effective_num_units * tick_count;

ALTER TABLE identities ADD COLUMN marriage_idx INTEGER;
