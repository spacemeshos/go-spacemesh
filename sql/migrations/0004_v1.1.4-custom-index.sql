CREATE INDEX atxs_by_coinbase_by_epoch_desc ON atxs (coinbase, epoch desc);
CREATE INDEX atxs_by_epoch_by_coinbase ON atxs (epoch, coinbase);
