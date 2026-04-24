ALTER TABLE support_requests
DROP CONSTRAINT support_requests_audience_check;

ALTER TABLE support_requests
ADD CONSTRAINT support_requests_audience_check
CHECK (audience IN ('friends', 'city', 'community'));
