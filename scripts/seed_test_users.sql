-- ============================================================
-- Test seed: Irish users for suggestions endpoint testing
-- Password for all accounts: Password1!
--
-- Usage (dev only):
--   psql $DATABASE_URL -f scripts/seed_test_users.sql
--
-- Teardown:
--   DELETE FROM users WHERE id::text LIKE 'a1000000-%';
-- ============================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Step 1: Insert users
DO $$
DECLARE
  pw text;
BEGIN
  pw := crypt('Password1!', gen_salt('bf'));

  INSERT INTO users (id, first_name, last_name, email, password_hash, city, country, lat, lng, sober_since, discovery_radius_km)
  VALUES
    ('a1000000-0000-0000-0000-000000000001', 'Aoife',     'Murphy',   'aoife.murphy@example.com',     pw, 'Portlaoise',   'Ireland', 53.029160, -7.320510, '2022-01-15', 100),
    ('a1000000-0000-0000-0000-000000000002', 'Ciaran',    'Ryan',     'ciaran.ryan@example.com',      pw, 'Carlow',       'Ireland', 52.840022, -6.927866, '2021-03-10', 100),
    ('a1000000-0000-0000-0000-000000000003', 'Siobhan',   'O''Brien', 'siobhan.obrien@example.com',   pw, 'Dublin',       'Ireland', 53.349804, -6.260310, '2020-06-01', 100),
    ('a1000000-0000-0000-0000-000000000004', 'Declan',    'Walsh',    'declan.walsh@example.com',     pw, 'Cork',         'Ireland', 51.896893, -8.486316, '2023-02-20', 100),
    ('a1000000-0000-0000-0000-000000000005', 'Niamh',     'Byrne',    'niamh.byrne@example.com',      pw, 'Athy',         'Ireland', 52.993279, -6.981844, '2022-07-05', 100),
    ('a1000000-0000-0000-0000-000000000006', 'Seamus',    'Kelly',    'seamus.kelly@example.com',     pw, 'Galway',       'Ireland', 53.276685, -9.045096, '2021-11-30', 100),
    ('a1000000-0000-0000-0000-000000000007', 'Grainne',   'Doyle',    'grainne.doyle@example.com',    pw, 'Athlone',      'Ireland', 53.430401, -7.941021, '2020-09-14', 100),
    ('a1000000-0000-0000-0000-000000000008', 'Padraic',   'Farrell',  'padraic.farrell@example.com',  pw, 'Monasterevin', 'Ireland', 53.142826, -7.064399, '2023-04-01', 100),
    ('a1000000-0000-0000-0000-000000000009', 'Orla',      'Kavanagh', 'orla.kavanagh@example.com',    pw, 'Stradbally',   'Ireland', 53.014746, -7.148406, '2022-12-25', 100),
    ('a1000000-0000-0000-0000-000000000010', 'Brendan',   'Dunne',    'brendan.dunne@example.com',    pw, 'Abbeyleix',    'Ireland', 52.914571, -7.350522, '2021-08-18', 100),
    ('a1000000-0000-0000-0000-000000000011', 'Fionnuala', 'Phelan',   'fionnuala.phelan@example.com', pw, 'Kilkenny',     'Ireland', 52.654245, -7.244605, '2020-04-22', 100)
  ON CONFLICT (email) DO NOTHING;
END;
$$;

-- Step 2: Assign interests (varied combos to exercise both interest similarity and proximity scoring)
-- Available: Running, Coffee, Hiking, Music, Travel, Mindfulness, Yoga, Cycling, Reading, Film, Cooking, Photography, Gaming, Art, Volunteering

-- Aoife, Portlaoise
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000001', id FROM interests
WHERE name IN ('Running', 'Hiking', 'Music', 'Travel')
ON CONFLICT DO NOTHING;

-- Ciaran, Carlow
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000002', id FROM interests
WHERE name IN ('Coffee', 'Music', 'Mindfulness', 'Yoga')
ON CONFLICT DO NOTHING;

-- Siobhan, Dublin
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000003', id FROM interests
WHERE name IN ('Running', 'Cycling', 'Reading', 'Gaming')
ON CONFLICT DO NOTHING;

-- Declan, Cork
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000004', id FROM interests
WHERE name IN ('Travel', 'Film', 'Cooking', 'Photography')
ON CONFLICT DO NOTHING;

-- Niamh, Athy
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000005', id FROM interests
WHERE name IN ('Running', 'Music', 'Travel', 'Volunteering')
ON CONFLICT DO NOTHING;

-- Seamus, Galway
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000006', id FROM interests
WHERE name IN ('Hiking', 'Mindfulness', 'Reading', 'Art')
ON CONFLICT DO NOTHING;

-- Grainne, Athlone
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000007', id FROM interests
WHERE name IN ('Coffee', 'Music', 'Film', 'Gaming')
ON CONFLICT DO NOTHING;

-- Padraic, Monasterevin
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000008', id FROM interests
WHERE name IN ('Running', 'Hiking', 'Cycling', 'Volunteering')
ON CONFLICT DO NOTHING;

-- Orla, Stradbally
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000009', id FROM interests
WHERE name IN ('Music', 'Mindfulness', 'Yoga', 'Art')
ON CONFLICT DO NOTHING;

-- Brendan, Abbeyleix
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000010', id FROM interests
WHERE name IN ('Coffee', 'Travel', 'Reading', 'Photography')
ON CONFLICT DO NOTHING;

-- Fionnuala, Kilkenny
INSERT INTO user_interests (user_id, interest_id)
SELECT 'a1000000-0000-0000-0000-000000000011', id FROM interests
WHERE name IN ('Running', 'Film', 'Cooking', 'Gaming')
ON CONFLICT DO NOTHING;

-- Step 3: Rebuild interest_vec for the test users.
-- Replicates the Go logic: IDF = ln(total / (count + 1)), then L2-normalise.
-- Uses all users for IDF weights (same as the live app) but only updates test rows.
WITH
  total AS (
    SELECT COUNT(*)::float8 AS n FROM users
  ),
  interest_stats AS (
    SELECT
      i.id,
      ROW_NUMBER() OVER (ORDER BY i.id) AS pos,
      LN((SELECT n FROM total) / (COUNT(ui.user_id)::float8 + 1)) AS idf
    FROM interests i
    LEFT JOIN user_interests ui ON ui.interest_id = i.id
    GROUP BY i.id
  ),
  user_raw_vecs AS (
    SELECT
      u.id AS user_id,
      ARRAY_AGG(
        CASE WHEN EXISTS (
          SELECT 1 FROM user_interests ui2
          WHERE ui2.user_id = u.id AND ui2.interest_id = ist.id
        ) THEN ist.idf ELSE 0.0 END
        ORDER BY ist.pos
      ) AS raw_vec
    FROM users u
    CROSS JOIN interest_stats ist
    WHERE u.id::text LIKE 'a1000000-%'
    GROUP BY u.id
  ),
  norms AS (
    SELECT
      user_id,
      raw_vec,
      SQRT((SELECT SUM(v * v) FROM UNNEST(raw_vec) AS v)) AS norm
    FROM user_raw_vecs
  )
UPDATE users
SET interest_vec = (
  SELECT ARRAY_AGG(
    CASE WHEN n.norm = 0 THEN 0.0 ELSE elem / n.norm END
    ORDER BY ord
  )
  FROM UNNEST(n.raw_vec) WITH ORDINALITY AS t(elem, ord)
)
FROM norms n
WHERE users.id = n.user_id;
