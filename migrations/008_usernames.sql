ALTER TABLE users
  ADD COLUMN IF NOT EXISTS username TEXT;

WITH prepared AS (
  SELECT
    id,
    CASE
      WHEN LENGTH(TRIM(BOTH '.' FROM REGEXP_REPLACE(LOWER(COALESCE(first_name, '') || COALESCE(last_name, '')), '[^a-z0-9._]+', '', 'g'))) >= 3
        THEN LEFT(TRIM(BOTH '.' FROM REGEXP_REPLACE(LOWER(COALESCE(first_name, '') || COALESCE(last_name, '')), '[^a-z0-9._]+', '', 'g')), 20)
      WHEN LENGTH(TRIM(BOTH '.' FROM REGEXP_REPLACE(LOWER(SPLIT_PART(email, '@', 1)), '[^a-z0-9._]+', '', 'g'))) >= 3
        THEN LEFT(TRIM(BOTH '.' FROM REGEXP_REPLACE(LOWER(SPLIT_PART(email, '@', 1)), '[^a-z0-9._]+', '', 'g')), 20)
      ELSE 'user' || SUBSTRING(REPLACE(id::text, '-', '') FROM 1 FOR 8)
    END AS candidate
  FROM users
),
deduped AS (
  SELECT
    id,
    candidate,
    ROW_NUMBER() OVER (PARTITION BY candidate ORDER BY id) AS rn
  FROM prepared
)
UPDATE users u
SET username = CASE
  WHEN d.rn = 1 THEN d.candidate
  ELSE LEFT(d.candidate, 20 - LENGTH(d.rn::text)) || d.rn::text
END
FROM deduped d
WHERE u.id = d.id
  AND COALESCE(u.username, '') = '';

ALTER TABLE users
  ALTER COLUMN username SET NOT NULL;

ALTER TABLE users
  ADD CONSTRAINT users_username_format_chk
  CHECK (username ~ '^[a-z0-9._]{3,20}$');

CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique_idx
  ON users (username);
