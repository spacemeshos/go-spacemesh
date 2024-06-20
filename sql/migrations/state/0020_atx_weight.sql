ALTER TABLE atxs ADD COLUMN weight INTEGER;
INSERT INTO atxs (weight) SELECT effective_num_units * tick_count FROM atxs;
