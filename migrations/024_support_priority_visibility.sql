ALTER TABLE support_requests
ADD COLUMN IF NOT EXISTS priority_visibility BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS priority_expires_at TIMESTAMP NULL;

CREATE INDEX IF NOT EXISTS idx_support_requests_priority_expires_at
ON support_requests(priority_expires_at DESC)
WHERE priority_visibility = TRUE;
