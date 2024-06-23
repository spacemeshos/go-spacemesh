ALTER TABLE atxs ADD COLUMN weight INTEGER;
UPDATE atxs SET weight = effective_num_units * tick_count;
