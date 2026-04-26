CREATE TABLE IF NOT EXISTS event_categories (
    slug TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0
);

INSERT INTO event_categories (slug, label, sort_order) VALUES
    ('recovery', 'Recovery', 10),
    ('coffee', 'Coffee', 20),
    ('running', 'Running', 30),
    ('wellness', 'Wellness', 40),
    ('outdoors', 'Outdoors', 50),
    ('community', 'Community', 60),
    ('books', 'Books', 70),
    ('arts', 'Arts', 80),
    ('food', 'Food', 90),
    ('volunteering', 'Volunteering', 100)
ON CONFLICT (slug) DO NOTHING;

ALTER TABLE meetups
    ALTER COLUMN starts_at TYPE TIMESTAMPTZ USING starts_at AT TIME ZONE 'UTC';

ALTER TABLE meetups
    ADD COLUMN IF NOT EXISTS category_slug TEXT REFERENCES event_categories(slug) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS event_type TEXT NOT NULL DEFAULT 'in_person',
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'published',
    ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'public',
    ADD COLUMN IF NOT EXISTS country TEXT,
    ADD COLUMN IF NOT EXISTS venue_name TEXT,
    ADD COLUMN IF NOT EXISTS address_line_1 TEXT,
    ADD COLUMN IF NOT EXISTS address_line_2 TEXT,
    ADD COLUMN IF NOT EXISTS how_to_find_us TEXT,
    ADD COLUMN IF NOT EXISTS online_url TEXT,
    ADD COLUMN IF NOT EXISTS cover_image_url TEXT,
    ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'UTC',
    ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS waitlist_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS waitlist_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS saved_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE meetups
SET category_slug = COALESCE(category_slug, 'community'),
    published_at = COALESCE(published_at, NOW()),
    updated_at = COALESCE(updated_at, NOW()),
    created_at = COALESCE(created_at, NOW())
WHERE category_slug IS NULL
   OR published_at IS NULL
   OR updated_at IS NULL
   OR created_at IS NULL;

CREATE TABLE IF NOT EXISTS event_hosts (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'co_host',
    PRIMARY KEY (meetup_id, user_id)
);

INSERT INTO event_hosts (meetup_id, user_id, role)
SELECT id, organiser_id, 'organizer'
FROM meetups
ON CONFLICT (meetup_id, user_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS event_waitlist (
    meetup_id UUID NOT NULL REFERENCES meetups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (meetup_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_meetups_status_starts_at
    ON meetups(status, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_category_starts_at
    ON meetups(category_slug, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_lat_lng
    ON meetups(lat, lng);

CREATE INDEX IF NOT EXISTS idx_event_hosts_meetup_id
    ON event_hosts(meetup_id);

CREATE INDEX IF NOT EXISTS idx_event_waitlist_meetup_id
    ON event_waitlist(meetup_id);
