ALTER TABLE activesets ADD epoch INT DEFAULT 0 NOT NULL;
CREATE INDEX activesets_by_epoch ON activesets (epoch asc);
UPDATE activesets SET epoch = 7 WHERE epoch = 0;
