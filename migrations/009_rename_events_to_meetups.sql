ALTER TABLE events RENAME TO meetups;
ALTER TABLE event_attendees RENAME TO meetup_attendees;
ALTER TABLE meetup_attendees RENAME COLUMN event_id TO meetup_id;

ALTER TABLE meetups RENAME CONSTRAINT events_pkey TO meetups_pkey;
ALTER TABLE meetups RENAME CONSTRAINT events_organiser_id_fkey TO meetups_organiser_id_fkey;
ALTER TABLE meetup_attendees RENAME CONSTRAINT event_attendees_pkey TO meetup_attendees_pkey;
ALTER TABLE meetup_attendees RENAME CONSTRAINT event_attendees_event_id_fkey TO meetup_attendees_meetup_id_fkey;
ALTER TABLE meetup_attendees RENAME CONSTRAINT event_attendees_user_id_fkey TO meetup_attendees_user_id_fkey;

ALTER INDEX idx_events_city RENAME TO idx_meetups_city;
ALTER INDEX idx_events_starts_at RENAME TO idx_meetups_starts_at;
