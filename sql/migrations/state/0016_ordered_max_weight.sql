DROP INDEX IF EXISTS atxs_by_epoch_by_pubkey;
CREATE INDEX IF NOT EXISTS atxs_by_epoch_pubkey_weight ON atxs (epoch, pubkey, effective_num_units*tick_count DESC);