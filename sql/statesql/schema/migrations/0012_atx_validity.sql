-- For distributed POST verification
ALTER TABLE atxs ADD COLUMN validity INTEGER DEFAULT false;
UPDATE atxs SET validity = 1;
