ALTER TABLE feed_impressions
    DROP CONSTRAINT IF EXISTS feed_impressions_feed_mode_check;

ALTER TABLE feed_impressions
    ADD CONSTRAINT feed_impressions_feed_mode_check
        CHECK (feed_mode IN ('home'));

ALTER TABLE feed_events
    DROP CONSTRAINT IF EXISTS feed_events_feed_mode_check;

ALTER TABLE feed_events
    ADD CONSTRAINT feed_events_feed_mode_check
        CHECK (feed_mode IN ('home'));
