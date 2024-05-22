ALTER TABLE initial_post ADD COLUMN challenge BLOB NOT NULL DEFAULT x'0000000000000000000000000000000000000000000000000000000000000000';

ALTER TABLE initial_post RENAME TO post;

CREATE TABLE poet_certificates
(
    node_id      BLOB NOT NULL,
    certifier_id BLOB NOT NULL,
    certificate  BLOB NOT NULL,
    signature    BLOB NOT NULL
);

CREATE UNIQUE INDEX idx_poet_certificates ON poet_certificates (node_id, certifier_id);
