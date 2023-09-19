CREATE TABLE rewards_atxs
(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    atx_id      CHAR(32),
    coinbase    CHAR(24),
    layer       INT NOT NULL,
    total_reward UNSIGNED LONG INT,
    layer_reward UNSIGNED LONG INT
);

CREATE INDEX rewards_atxs_by_coinbase ON rewards_atxs (coinbase, layer);
CREATE INDEX rewards_atxs_by_atx_id ON rewards_atxs (atx_id, layer);
CREATE INDEX rewards_atxs_by_layer ON rewards_atxs (layer asc);
