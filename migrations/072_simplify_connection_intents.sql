ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_connection_intents_chk;

UPDATE users
SET connection_intents = CASE
    WHEN connection_intents && ARRAY['dating']::TEXT[] THEN ARRAY['friends', 'dating']::TEXT[]
    ELSE ARRAY['friends']::TEXT[]
END;

ALTER TABLE users
    ALTER COLUMN connection_intents SET DEFAULT ARRAY['friends']::TEXT[];

ALTER TABLE users
    ADD CONSTRAINT users_connection_intents_chk
    CHECK (
        cardinality(connection_intents) BETWEEN 1 AND 2
        AND connection_intents <@ ARRAY['friends', 'dating']::TEXT[]
        AND connection_intents @> ARRAY['friends']::TEXT[]
    );
