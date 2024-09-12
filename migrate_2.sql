ATTACH DATABASE '/home/dd/spacemesh/state.sql' AS pruned;

CREATE TABLE atx_blobs
(
    id CHAR(32),
    atx BLOB
, version INTEGER);
CREATE UNIQUE INDEX atx_blobs_id ON atx_blobs (id);