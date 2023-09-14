DELETE FROM proposals WHERE layer < 19000;
DELETE FROM proposal_transactions WHERE layer < 19000;
UPDATE certificates SET cert = NULL WHERE layer < 19000;
UPDATE ballots SET ballot = prune_actives(ballot);

