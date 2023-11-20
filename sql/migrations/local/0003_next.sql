ALTER TABLE initial_post ADD COLUMN challenge        VARCHAR NOT NULL;

ALTER TABLE initial_post RENAME TO post;

CREATE TABLE poet_certificates
(
    node_id     CHAR(32) NOT NULL,
    poet_url    VARCHAR NOT NULL,
    certificate VARCHAR NOT NULL
);

CREATE UNIQUE INDEX idx_poet_certificates ON poet_certificates (node_id, poet_url);
