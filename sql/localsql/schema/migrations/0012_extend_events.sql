DROP TABLE events;

CREATE TABLE events
(
    id        CHAR(32) NOT NULL,
    timestamp INTEGER  NOT NULL,
    kind      INTEGER  NOT NULL,
    event     TEXT     NOT NULL
);
CREATE INDEX events_by_id_timestamp ON events (id, timestamp);
CREATE INDEX events_by_kind ON events (kind);