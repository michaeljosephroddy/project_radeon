ALTER TABLE dating_profile_photos
    DROP CONSTRAINT IF EXISTS dating_profile_photos_profile_id_position_key;

ALTER TABLE dating_profile_photos
    ADD CONSTRAINT dating_profile_photos_profile_id_position_key
    UNIQUE (profile_id, position)
    DEFERRABLE INITIALLY IMMEDIATE;
