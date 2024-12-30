--- Table for storing identities' states.
CREATE TABLE states
(
    id        CHAR(32) NOT NULL,
    timestamp INTEGER NOT NULL,
    state     TEXT NOT NULL
);


--- Table for storing identities' proposals.
CREATE TABLE state_proposals
(
    id       CHAR(32) NOT NULL,
    layer    INTEGER NOT NULL,
    proposal BLOB NOT NULL
);

--- Table for storing identities' eligibilities.
CREATE TABLE state_eligibilities
(
    id        CHAR(32) NOT NULL,
    layer     INTEGER NOT NULL,
    j         INTEGER NOT NULL,
    signature CHAR(80)
);
