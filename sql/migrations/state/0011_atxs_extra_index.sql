-- retrieve epoch info faster
CREATE INDEX atxs_by_epoch_id on atxs (epoch, id);
