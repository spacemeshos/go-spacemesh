CREATE TABLE evicted_mempool (
  txid CHAR(32) NOT NULL,
  time TIMESTAMP NOT NULL,
  PRIMARY KEY txid
);
