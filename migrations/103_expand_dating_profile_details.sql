ALTER TABLE dating_profiles
    ADD COLUMN IF NOT EXISTS relationship_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS gender TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sexuality TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pronouns TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ethnicity TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS children_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pets TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS religious_belief TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS languages_spoken TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD COLUMN IF NOT EXISTS political_view TEXT NOT NULL DEFAULT '';

UPDATE dating_profiles
SET relationship_goal = CASE relationship_goal
    WHEN 'casual' THEN 'short_term_open_to_long_term'
    WHEN 'open_to_explore' THEN 'still_figuring_it_out'
    ELSE relationship_goal
END;

UPDATE dating_profiles
SET children_status = CASE kids_status
    WHEN 'have_kids' THEN 'have_children'
    ELSE children_status
END
WHERE children_status = '';

ALTER TABLE dating_profiles
    DROP CONSTRAINT IF EXISTS dating_profiles_relationship_goal_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_relationship_type_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_gender_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_sexuality_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_pronouns_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_ethnicity_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_children_status_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_pets_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_religious_belief_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_languages_spoken_chk,
    DROP CONSTRAINT IF EXISTS dating_profiles_political_view_chk;

ALTER TABLE dating_profiles
    ADD CONSTRAINT dating_profiles_relationship_goal_chk CHECK (
        relationship_goal IN ('', 'long_term', 'life_partner', 'short_term_open_to_long_term', 'still_figuring_it_out', 'new_sober_connections')
    ),
    ADD CONSTRAINT dating_profiles_relationship_type_chk CHECK (
        relationship_type IN ('', 'monogamous', 'open_relationship', 'other')
    ),
    ADD CONSTRAINT dating_profiles_gender_chk CHECK (
        gender IN ('', 'woman', 'man', 'non_binary', 'other')
    ),
    ADD CONSTRAINT dating_profiles_sexuality_chk CHECK (
        sexuality IN ('', 'straight', 'gay', 'lesbian', 'bisexual', 'other')
    ),
    ADD CONSTRAINT dating_profiles_pronouns_chk CHECK (
        pronouns IN ('', 'she_her', 'he_him', 'they_them', 'other')
    ),
    ADD CONSTRAINT dating_profiles_ethnicity_chk CHECK (
        ethnicity IN ('', 'asian', 'black', 'hispanic_latino', 'middle_eastern', 'mixed', 'native_indigenous', 'white', 'other')
    ),
    ADD CONSTRAINT dating_profiles_children_status_chk CHECK (
        children_status IN ('', 'have_children', 'have_children_want_more', 'have_children_dont_want_more', 'want_children', 'dont_want_children', 'open_to_children', 'not_sure')
    ),
    ADD CONSTRAINT dating_profiles_pets_chk CHECK (
        pets IN ('', 'have_pets', 'want_pets', 'like_pets', 'allergic_to_pets', 'not_a_pet_person')
    ),
    ADD CONSTRAINT dating_profiles_religious_belief_chk CHECK (
        religious_belief IN ('', 'agnostic', 'atheist', 'buddhist', 'christian', 'hindu', 'jewish', 'muslim', 'sikh', 'spiritual', 'other')
    ),
    ADD CONSTRAINT dating_profiles_languages_spoken_chk CHECK (
        cardinality(languages_spoken) <= 5
        AND languages_spoken <@ ARRAY[
            'english', 'irish', 'spanish', 'french', 'german', 'italian', 'portuguese', 'dutch',
            'polish', 'romanian', 'lithuanian', 'latvian', 'estonian', 'russian', 'ukrainian',
            'czech', 'slovak', 'hungarian', 'greek', 'turkish', 'arabic', 'hebrew',
            'persian_farsi', 'hindi', 'urdu', 'punjabi', 'bengali', 'gujarati', 'tamil',
            'telugu', 'malayalam', 'marathi', 'nepali', 'mandarin', 'cantonese', 'japanese',
            'korean', 'vietnamese', 'thai', 'indonesian', 'malay', 'filipino_tagalog',
            'swahili', 'yoruba', 'igbo', 'amharic', 'somali', 'afrikaans', 'other'
        ]::TEXT[]
    ),
    ADD CONSTRAINT dating_profiles_political_view_chk CHECK (
        political_view IN ('', 'liberal', 'moderate', 'conservative', 'not_political', 'other')
    );
