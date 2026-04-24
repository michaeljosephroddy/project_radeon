-- Drop support_mode from users (modes removed entirely; availability is on/off only)
ALTER TABLE users
    DROP COLUMN IF EXISTS support_mode;

-- Rename need_company -> need_in_person_help in support_requests, update constraint
ALTER TABLE support_requests
    DROP CONSTRAINT IF EXISTS support_requests_type_check;

UPDATE support_requests
    SET type = 'need_in_person_help'
    WHERE type = 'need_company';

ALTER TABLE support_requests
    ADD CONSTRAINT support_requests_type_check
    CHECK (type IN ('need_to_talk', 'need_distraction', 'need_encouragement', 'need_in_person_help'));

-- Rename nearby -> can_meet in support_responses, update constraint
ALTER TABLE support_responses
    DROP CONSTRAINT IF EXISTS support_responses_response_type_check;

UPDATE support_responses
    SET response_type = 'can_meet'
    WHERE response_type = 'nearby';

ALTER TABLE support_responses
    ADD CONSTRAINT support_responses_response_type_check
    CHECK (response_type IN ('can_chat', 'check_in_later', 'can_meet'));
