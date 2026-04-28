ALTER TABLE users
DROP COLUMN IF EXISTS is_available_to_support,
DROP COLUMN IF EXISTS support_updated_at,
DROP COLUMN IF EXISTS support_mode,
DROP COLUMN IF EXISTS support_modes;

DROP INDEX IF EXISTS idx_users_available_to_support;
