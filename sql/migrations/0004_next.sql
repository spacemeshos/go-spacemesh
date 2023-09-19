DELETE FROM proposals;
DELETE FROM proposal_transactions;
UPDATE certificates SET cert = NULL WHERE layer < 19000;
CREATE INDEX ballots_by_atx_by_layer ON ballots (atx, layer asc);
