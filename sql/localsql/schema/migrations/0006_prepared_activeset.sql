CREATE TABLE prepared_activeset
(
    kind          UNSIGNED INT NOT NULL,    
    epoch         UNSIGNED INT NOT NULL,
    id            CHAR(32) NOT NULL,
    weight        UNSIGNED INT NOT NULL,
    data          BLOB NOT NULL,
    PRIMARY KEY (kind, epoch)
) WITHOUT ROWID;