-- Add version column to make it easier to decode the blob
-- to the right version of ATX.
ALTER TABLE atx_blobs ADD COLUMN version INTEGER;
