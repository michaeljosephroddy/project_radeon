CREATE TEMP TABLE profile_interest_mappings (
    old_name TEXT PRIMARY KEY,
    new_name TEXT NOT NULL
) ON COMMIT DROP;

INSERT INTO profile_interest_mappings (old_name, new_name) VALUES
    ('Art', 'Art & Creativity'),
    ('Books', 'Reading'),
    ('Cycling', 'Fitness'),
    ('Film', 'Movies & TV'),
    ('Gym', 'Fitness'),
    ('Hiking', 'Outdoors'),
    ('Journaling', 'Mindfulness'),
    ('Live Music', 'Music'),
    ('Meditation', 'Mindfulness'),
    ('Meetups', 'Meetings'),
    ('Movies', 'Movies & TV'),
    ('Nature Walks', 'Outdoors'),
    ('Photography', 'Art & Creativity'),
    ('Running', 'Fitness'),
    ('Travel', 'Travel'),
    ('Yoga', 'Fitness')
ON CONFLICT (old_name) DO UPDATE
SET new_name = EXCLUDED.new_name;

INSERT INTO interests (name) VALUES
    ('Coffee'),
    ('Fitness'),
    ('Outdoors'),
    ('Reading'),
    ('Music'),
    ('Movies & TV'),
    ('Cooking'),
    ('Gaming'),
    ('Art & Creativity'),
    ('Mindfulness'),
    ('Meetings'),
    ('Volunteering'),
    ('Family'),
    ('Sports'),
    ('Travel'),
    ('Pets')
ON CONFLICT (name) DO NOTHING;

INSERT INTO user_interests (user_id, interest_id)
SELECT DISTINCT ui.user_id, new_interest.id
FROM user_interests ui
JOIN interests old_interest ON old_interest.id = ui.interest_id
JOIN profile_interest_mappings mapping ON mapping.old_name = old_interest.name
JOIN interests new_interest ON new_interest.name = mapping.new_name
ON CONFLICT DO NOTHING;

DELETE FROM interests
WHERE name NOT IN (
    'Coffee',
    'Fitness',
    'Outdoors',
    'Reading',
    'Music',
    'Movies & TV',
    'Cooking',
    'Gaming',
    'Art & Creativity',
    'Mindfulness',
    'Meetings',
    'Volunteering',
    'Family',
    'Sports',
    'Travel',
    'Pets'
);
