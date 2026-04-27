CREATE INDEX IF NOT EXISTS idx_support_requests_channel_status_created_at
ON support_requests(channel, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_requester_channel_created_at
ON support_requests(requester_id, channel, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_requests_routing_status_created_at
ON support_requests(routing_status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_offers_responder_status_expires_at
ON support_offers(responder_id, status, expires_at, offered_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_offers_request_status
ON support_offers(support_request_id, status, offered_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_sessions_requester_status_created_at
ON support_sessions(requester_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_sessions_responder_status_created_at
ON support_sessions(responder_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_events_request_created_at
ON support_events(support_request_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_events_session_created_at
ON support_events(support_session_id, created_at DESC);
