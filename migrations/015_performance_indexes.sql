-- Enable trigram extension for efficient ILIKE searches on username and chat name
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- post_reactions: cover the per-post COUNT subquery (post_id, type) and the
-- toggle EXISTS check (post_id, user_id, type is already covered by the UNIQUE constraint)
CREATE INDEX IF NOT EXISTS idx_post_reactions_post_id_type
    ON post_reactions(post_id, type);

-- posts: cover ORDER BY created_at DESC on the global feed and per-user timeline
CREATE INDEX IF NOT EXISTS idx_posts_created_at_desc
    ON posts(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_posts_user_id_created_at
    ON posts(user_id, created_at DESC);

-- messages: cover the cursor-based pagination query and the LATERAL last-message
-- preview in ListChats (both filter by chat_id and sort by sent_at DESC)
CREATE INDEX IF NOT EXISTS idx_messages_chat_id_sent_at
    ON messages(chat_id, sent_at DESC);

-- chat_members: cover the per-user inbox lookup (user_id first so ListChats can
-- seek directly to the caller's memberships without scanning all members)
CREATE INDEX IF NOT EXISTS idx_chat_members_user_id
    ON chat_members(user_id);

-- meetup_attendees: cover the attendee COUNT subquery and the capacity check
CREATE INDEX IF NOT EXISTS idx_meetup_attendees_meetup_id
    ON meetup_attendees(meetup_id);

-- support_responses: cover the per-request has_responded EXISTS subquery
CREATE INDEX IF NOT EXISTS idx_support_responses_responder_request
    ON support_responses(responder_id, support_request_id);

-- users: partial index for the available-to-support count query
CREATE INDEX IF NOT EXISTS idx_users_available_to_support
    ON users(id) WHERE is_available_to_support = true;

-- users: trigram index for username ILIKE '%query%' searches in Discover
CREATE INDEX IF NOT EXISTS idx_users_username_trgm
    ON users USING GIN(username gin_trgm_ops);

-- users: btree index for exact city filter in Discover and support visibility
CREATE INDEX IF NOT EXISTS idx_users_city
    ON users(city);
