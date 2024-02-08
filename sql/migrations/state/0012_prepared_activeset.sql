-- For prepared activeset in miner module.
ALTER TABLE activesets ADD COLUMN prepared BOOL;
ALTER TABLE activesets ADD COLUMN weight INTEGER;
