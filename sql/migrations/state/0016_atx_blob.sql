CREATE TABLE atx_blobs
(
    id CHAR(32) PRIMARY KEY,
    atx BLOB
);

INSERT INTO atx_blobs (id, atx) SELECT id, atx FROM atxs;

ALTER TABLE atxs DROP COLUMN atx;
