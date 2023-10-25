ALTER TABLE activesets ADD epoch INT DEFAULT 0 NOT NULL;
CREATE INDEX activesets_by_epoch ON activesets (epoch asc);
UPDATE activesets SET epoch = 7 WHERE epoch = 0;
ALTER TABLE rewards RENAME TO rewards_old;
CREATE TABLE rewards
(
    pubkey       CHAR(32),
    coinbase     CHAR(24) NOT NULL,
    layer        INT NOT NULL,
    total_reward UNSIGNED LONG INT,
    layer_reward UNSIGNED LONG INT,
    PRIMARY KEY (pubkey, layer)
) WITHOUT ROWID;
CREATE INDEX rewards_by_coinbase ON rewards (coinbase, layer);
CREATE INDEX rewards_by_layer ON rewards (layer asc);
INSERT INTO rewards SELECT * FROM rewards_old;
DROP TABLE rewards_old;