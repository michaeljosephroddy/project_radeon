ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bio TEXT;

CREATE TABLE IF NOT EXISTS interests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS user_interests (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    interest_id UUID NOT NULL REFERENCES interests(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, interest_id)
);

CREATE INDEX IF NOT EXISTS idx_user_interests_user_id
    ON user_interests(user_id);

CREATE INDEX IF NOT EXISTS idx_user_interests_interest_id
    ON user_interests(interest_id);

INSERT INTO interests (name) VALUES
    ('Art'),
    ('Books'),
    ('Coffee'),
    ('Cooking'),
    ('Gaming'),
    ('Gym'),
    ('Hiking'),
    ('Journaling'),
    ('Live Music'),
    ('Meditation'),
    ('Meetups'),
    ('Movies'),
    ('Nature Walks'),
    ('Running'),
    ('Volunteering'),
    ('Yoga')
ON CONFLICT (name) DO NOTHING;
