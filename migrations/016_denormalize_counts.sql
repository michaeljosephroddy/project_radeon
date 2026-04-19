-- Denormalize frequently-read aggregate counts so they are never recomputed
-- live on every request. Each count is maintained by the write paths via
-- UPDATE ... SET count = count ± 1 inside the same transaction as the row
-- insert/delete, keeping reads O(1) instead of O(n) subqueries.

-- friend_count on users: number of accepted friendships for a user
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS friend_count INT NOT NULL DEFAULT 0;

UPDATE users u
SET friend_count = (
    SELECT COUNT(*)
    FROM friendships f
    WHERE (f.user_a_id = u.id OR f.user_b_id = u.id)
        AND f.status = 'accepted'
);

-- attendee_count on meetups: number of confirmed RSVPs
ALTER TABLE meetups
    ADD COLUMN IF NOT EXISTS attendee_count INT NOT NULL DEFAULT 0;

UPDATE meetups m
SET attendee_count = (
    SELECT COUNT(*) FROM meetup_attendees WHERE meetup_id = m.id
);

-- response_count on support_requests: number of responses received
ALTER TABLE support_requests
    ADD COLUMN IF NOT EXISTS response_count INT NOT NULL DEFAULT 0;

UPDATE support_requests sr
SET response_count = (
    SELECT COUNT(*) FROM support_responses WHERE support_request_id = sr.id
);
