-- Ensure every poll vote references an option from the same poll.

DELETE FROM poll_votes pv
WHERE NOT EXISTS (
    SELECT 1
    FROM poll_options po
    WHERE po.id = pv.option_id
      AND po.poll_id = pv.poll_id
);

DO $$
BEGIN
    ALTER TABLE poll_options
        ADD CONSTRAINT poll_options_poll_id_id_key UNIQUE (poll_id, id);
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE poll_votes
        ADD CONSTRAINT poll_votes_poll_option_fk
        FOREIGN KEY (poll_id, option_id)
        REFERENCES poll_options(poll_id, id)
        ON DELETE CASCADE;
EXCEPTION
    WHEN duplicate_object THEN
        NULL;
END $$;
