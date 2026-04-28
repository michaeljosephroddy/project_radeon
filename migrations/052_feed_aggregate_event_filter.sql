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

    IF TG_TABLE_NAME = 'feed_events' THEN
        IF NEW.event_type <> 'like' THEN
            RETURN NEW;
        END IF;

        IF NEW.item_kind = 'post' THEN
            PERFORM enqueue_feed_aggregate_job('post', NEW.item_id);
        ELSIF NEW.item_kind = 'reshare' THEN
            PERFORM enqueue_feed_aggregate_job('share', NEW.item_id);
        END IF;
        RETURN NEW;
    END IF;

    IF TG_TABLE_NAME = 'feed_impressions' OR TG_TABLE_NAME = 'feed_hidden_posts' THEN
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
