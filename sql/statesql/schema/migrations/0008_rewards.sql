DROP INDEX rewards_by_layer;
ALTER TABLE rewards RENAME TO rewards_old;
CREATE TABLE rewards
(
    pubkey       CHAR(32),
    coinbase     CHAR(24) NOT NULL,
    layer        INT NOT NULL,
    total_reward UNSIGNED LONG INT,
    layer_reward UNSIGNED LONG INT,
    PRIMARY KEY (pubkey, layer)
);
CREATE INDEX rewards_by_coinbase ON rewards (coinbase, layer);
CREATE INDEX rewards_by_layer ON rewards (layer asc);
INSERT INTO rewards (coinbase, layer, total_reward, layer_reward) SELECT coinbase, layer, total_reward, layer_reward FROM rewards_old;
DROP TABLE rewards_old;
