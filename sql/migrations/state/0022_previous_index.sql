-- atxs.id, atxs.epoch, atxs.pubkey, atxs.sequence, atxs.nonce, atxs.effective_num_units,
-- atxs.base_tick_height, atxs.commitment_atx
UPDATE atxs
SET commitment_atx = (
    SELECT commitment_atx
    FROM atxs AS catxs
    WHERE catxs.pubkey = atxs.pubkey
    ORDER BY epoch ASC
    LIMIT 1
)
WHERE epoch >= 27;
