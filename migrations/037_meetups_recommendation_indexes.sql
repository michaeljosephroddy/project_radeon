CREATE INDEX IF NOT EXISTS idx_meetups_status_visibility_starts_at
    ON meetups(status, visibility, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetups_event_type_starts_at
    ON meetups(event_type, starts_at);

CREATE INDEX IF NOT EXISTS idx_meetup_attendees_user_meetup_id
    ON meetup_attendees(user_id, meetup_id);
