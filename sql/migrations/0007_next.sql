ALTER TABLE activesets ADD epoch INT DEFAULT 0 NOT NULL;
CREATE INDEX activesets_by_epoch ON activesets (epoch asc);
UPDATE activesets SET epoch = 7 WHERE epoch = 0;

CREATE TABLE nipost
(
    id            CHAR(32) NOT NULL,
    epoch         UNSIGNED INT NOT NULL,
    sequence      UNSIGNED INT NOT NULL,
    prev_atx      CHAR(32) NOT NULL,
    pos_atx       CHAR(32) NOT NULL,
    commit_atx    CHAR(32),
    post_nonce    UNSIGNED INT,
    post_indices  VARCHAR,
    post_pow      UNSIGNED LONG INT,
    PRIMARY KEY (id, epoch)
) WITHOUT ROWID;