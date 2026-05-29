ALTER TABLE dating_profile_prompt_answers
    DROP CONSTRAINT IF EXISTS dating_profile_prompt_key_chk;

ALTER TABLE dating_profile_prompt_answers
    ADD CONSTRAINT dating_profile_prompt_key_chk CHECK (
        prompt_key IN (
            'small_thing_about_me',
            'friends_describe_me',
            'proud_of',
            'happiest_when',
            'simple_pleasure',
            'recovery_lifestyle',
            'best_part_sobriety',
            'ideal_sober_date',
            'sober_win',
            'how_i_reset',
            'looking_for',
            'green_flag',
            'great_first_date',
            'chemistry_when',
            'dating_intention',
            'make_time_for',
            'value_i_live_by',
            'matters_most',
            'feel_connected_when',
            'relationship_works_when',
            'perfect_sunday',
            'usually_find_me',
            'sober_weekend',
            'recharge',
            'next_adventure',
            'ask_me_about',
            'teach_me_about',
            'lets_debate',
            'make_me_laugh',
            'voice_note_includes'
        )
    );
