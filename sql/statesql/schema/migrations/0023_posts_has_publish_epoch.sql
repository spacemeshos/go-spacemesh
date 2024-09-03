ALTER TABLE posts ADD COLUMN publish_epoch UNSIGNED INT;

-- migrate data by iterating over all atxids in posts and inserting epoch for matching atx from atxs table by id column
UPDATE posts
SET publish_epoch = (
    SELECT epoch
    FROM atxs
    WHERE atxs.id = posts.atxid
);


CREATE INDEX posts_by_atxid_by_pubkey_epoch ON posts (pubkey, publish_epoch);
