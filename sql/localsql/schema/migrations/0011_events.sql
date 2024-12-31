--- Table for storing identities's events.
CREATE TABLE events
(
    id        CHAR(32) NOT NULL,
    timestamp INTEGER NOT NULL,
    event     TEXT NOT NULL
);
CREATE INDEX events_by_id_timestamp ON events (id, timestamp);

--- Table for storing identities' proposals.
CREATE TABLE proposals
(
    id       CHAR(32) NOT NULL,
    layer    INTEGER NOT NULL,
    proposal BLOB NOT NULL
);
CREATE INDEX proposals_by_id_layer ON proposals (id, layer);

--- Table for storing identities' eligibilities.
CREATE TABLE eligibilities
(
    id        CHAR(32) NOT NULL,
    layer     INTEGER NOT NULL,
    j         INTEGER NOT NULL,
    signature CHAR(80)
);
CREATE INDEX eligibilities_by_id_layer ON eligibilities (id, layer);
