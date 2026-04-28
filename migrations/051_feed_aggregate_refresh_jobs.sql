CREATE TABLE IF NOT EXISTS feed_aggregate_jobs (
    target_kind TEXT NOT NULL CHECK (target_kind IN ('post', 'share', 'author')),
    target_id UUID NOT NULL,
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    attempt_count INT NOT NULL DEFAULT 0,
    last_error TEXT,
    PRIMARY KEY (target_kind, target_id)
);

CREATE INDEX IF NOT EXISTS idx_feed_aggregate_jobs_available
    ON feed_aggregate_jobs(available_at ASC, queued_at ASC)
    WHERE claimed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_feed_aggregate_jobs_claimed
    ON feed_aggregate_jobs(claimed_at ASC)
    WHERE claimed_at IS NOT NULL;

CREATE OR REPLACE FUNCTION enqueue_feed_aggregate_job(target_kind_in TEXT, target_id_in UUID)
RETURNS VOID AS $$
BEGIN
    IF target_id_in IS NULL OR target_kind_in IS NULL THEN
        RETURN;
    END IF;

    INSERT INTO feed_aggregate_jobs (
        target_kind,
        target_id,
        queued_at,
        available_at,
        claimed_at,
        last_error
    ) VALUES (
        target_kind_in,
        target_id_in,
        NOW(),
        NOW(),
        NULL,
        NULL
    )
    ON CONFLICT (target_kind, target_id) DO UPDATE
    SET queued_at = EXCLUDED.queued_at,
        available_at = EXCLUDED.available_at,
        claimed_at = NULL,
        last_error = NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION trigger_enqueue_feed_aggregate_job()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME = 'posts' THEN
        IF TG_OP = 'INSERT' THEN
            PERFORM enqueue_feed_aggregate_job('post', NEW.id);
            RETURN NEW;
        END IF;

        PERFORM enqueue_feed_aggregate_job('author', OLD.user_id);
        RETURN OLD;
    END IF;

    IF TG_TABLE_NAME = 'post_shares' THEN
        IF TG_OP = 'INSERT' THEN
            PERFORM enqueue_feed_aggregate_job('share', NEW.id);
            RETURN NEW;
        END IF;

        PERFORM enqueue_feed_aggregate_job('author', OLD.user_id);
        RETURN OLD;
    END IF;

    IF TG_TABLE_NAME = 'feed_impressions' OR TG_TABLE_NAME = 'feed_events' OR TG_TABLE_NAME = 'feed_hidden_posts' THEN
        IF TG_OP = 'DELETE' THEN
            IF OLD.item_kind = 'post' THEN
                PERFORM enqueue_feed_aggregate_job('post', OLD.item_id);
            ELSIF OLD.item_kind = 'reshare' THEN
                PERFORM enqueue_feed_aggregate_job('share', OLD.item_id);
            END IF;
            RETURN OLD;
        END IF;

        IF NEW.item_kind = 'post' THEN
            PERFORM enqueue_feed_aggregate_job('post', NEW.item_id);
        ELSIF NEW.item_kind = 'reshare' THEN
            PERFORM enqueue_feed_aggregate_job('share', NEW.item_id);
        END IF;
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'post_reactions' OR TG_TABLE_NAME = 'comments' THEN
        IF TG_OP = 'DELETE' THEN
            PERFORM enqueue_feed_aggregate_job('post', OLD.post_id);
            RETURN OLD;
        END IF;

        PERFORM enqueue_feed_aggregate_job('post', NEW.post_id);
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'share_reactions' OR TG_TABLE_NAME = 'share_comments' THEN
        IF TG_OP = 'DELETE' THEN
            PERFORM enqueue_feed_aggregate_job('share', OLD.share_id);
            RETURN OLD;
        END IF;

        PERFORM enqueue_feed_aggregate_job('share', NEW.share_id);
        RETURN NEW;
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_posts_enqueue_feed_aggregate_job ON posts;
CREATE TRIGGER trg_posts_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON posts
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_post_shares_enqueue_feed_aggregate_job ON post_shares;
CREATE TRIGGER trg_post_shares_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON post_shares
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_feed_impressions_enqueue_feed_aggregate_job ON feed_impressions;
CREATE TRIGGER trg_feed_impressions_enqueue_feed_aggregate_job
AFTER INSERT OR UPDATE ON feed_impressions
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_feed_events_enqueue_feed_aggregate_job ON feed_events;
CREATE TRIGGER trg_feed_events_enqueue_feed_aggregate_job
AFTER INSERT ON feed_events
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_feed_hidden_posts_enqueue_feed_aggregate_job ON feed_hidden_posts;
CREATE TRIGGER trg_feed_hidden_posts_enqueue_feed_aggregate_job
AFTER INSERT OR UPDATE OR DELETE ON feed_hidden_posts
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_post_reactions_enqueue_feed_aggregate_job ON post_reactions;
CREATE TRIGGER trg_post_reactions_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON post_reactions
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_comments_enqueue_feed_aggregate_job ON comments;
CREATE TRIGGER trg_comments_enqueue_feed_aggregate_job
AFTER INSERT ON comments
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_share_reactions_enqueue_feed_aggregate_job ON share_reactions;
CREATE TRIGGER trg_share_reactions_enqueue_feed_aggregate_job
AFTER INSERT OR DELETE ON share_reactions
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();

DROP TRIGGER IF EXISTS trg_share_comments_enqueue_feed_aggregate_job ON share_comments;
CREATE TRIGGER trg_share_comments_enqueue_feed_aggregate_job
AFTER INSERT ON share_comments
FOR EACH ROW
EXECUTE FUNCTION trigger_enqueue_feed_aggregate_job();
