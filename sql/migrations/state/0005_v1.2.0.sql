DELETE FROM proposals;
DELETE FROM proposal_transactions;
UPDATE certificates SET cert = NULL WHERE layer < 20000;
