INSERT INTO event_categories (slug, label, sort_order) VALUES
    ('recovery', 'Recovery', 10),
    ('social', 'Social', 20),
    ('activity', 'Activities', 30),
    ('wellness', 'Wellness', 40),
    ('online', 'Online', 50),
    ('service', 'Service', 60)
ON CONFLICT (slug) DO UPDATE SET
    label = EXCLUDED.label,
    sort_order = EXCLUDED.sort_order;

UPDATE meetups
SET category_slug = CASE category_slug
    WHEN 'coffee' THEN 'social'
    WHEN 'food' THEN 'social'
    WHEN 'books' THEN 'social'
    WHEN 'arts' THEN 'social'
    WHEN 'community' THEN 'social'
    WHEN 'running' THEN 'activity'
    WHEN 'outdoors' THEN 'activity'
    WHEN 'volunteering' THEN 'service'
    ELSE category_slug
END
WHERE category_slug IN (
    'coffee',
    'food',
    'books',
    'arts',
    'community',
    'running',
    'outdoors',
    'volunteering'
);

DELETE FROM event_categories
WHERE slug IN (
    'coffee',
    'food',
    'books',
    'arts',
    'community',
    'running',
    'outdoors',
    'volunteering'
);
