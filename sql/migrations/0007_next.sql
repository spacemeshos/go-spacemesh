ALTER TABLE activesets ADD epoch INT DEFAULT 0 NOT NULL;
CREATE INDEX activesets_by_epoch ON activesets (epoch asc);