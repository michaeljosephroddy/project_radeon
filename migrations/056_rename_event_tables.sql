ALTER TABLE event_hosts RENAME TO meetup_hosts;
ALTER TABLE event_waitlist RENAME TO meetup_waitlist;

ALTER INDEX IF EXISTS idx_event_hosts_meetup_id RENAME TO idx_meetup_hosts_meetup_id;
ALTER INDEX IF EXISTS idx_event_waitlist_meetup_id RENAME TO idx_meetup_waitlist_meetup_id;
