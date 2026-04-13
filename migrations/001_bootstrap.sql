-- Migration: add password_hash to users and ensure defaults
-- Run this against your existing project_radeon database

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';

-- Add default UUID generation if not already set
ALTER TABLE users         ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE posts         ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE comments      ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE connections   ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE events        ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE conversations ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE messages      ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE post_reactions ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE interests     ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- Add created_at defaults
ALTER TABLE users         ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE posts         ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE comments      ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE connections   ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE conversations ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE messages      ALTER COLUMN sent_at     SET DEFAULT NOW();
ALTER TABLE event_attendees ALTER COLUMN rsvp_at   SET DEFAULT NOW();
ALTER TABLE conversation_members ALTER COLUMN joined_at SET DEFAULT NOW();

-- Seed interests
INSERT INTO interests (name) VALUES
  ('Running'), ('Coffee'), ('Hiking'), ('Music'), ('Travel'),
  ('Mindfulness'), ('Yoga'), ('Cycling'), ('Reading'), ('Film'),
  ('Cooking'), ('Photography'), ('Gaming'), ('Art'), ('Volunteering')
ON CONFLICT DO NOTHING;
