ALTER TABLE malfeasance_sync_state RENAME TO malfeasance_sync_state_old;

CREATE TABLE malfeasance_sync_state
(
  id INT NOT NULL PRIMARY KEY,
  timestamp INT NOT NULL
);

INSERT INTO malfeasance_sync_state (id, timestamp)
SELECT 1, timestamp FROM malfeasance_sync_state_old LIMIT 1;

DROP TABLE malfeasance_sync_state_old;
