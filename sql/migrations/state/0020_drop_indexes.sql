drop index atxs_by_epoch_by_pubkey_nonce;
drop index atxs_by_epoch_by_pubkey;
drop index atxs_by_pubkey_by_epoch_desc;

create index atxs_by_epoch_by_pubkey_wrapping_atx_id on atxs(pubkey, epoch desc, id);