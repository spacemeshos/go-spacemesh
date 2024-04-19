-- For distributed POST verification
ALTER TABLE atx_blobs ADD COLUMN version INTEGER;
UPDATE atx_blobs SET version = 1;
