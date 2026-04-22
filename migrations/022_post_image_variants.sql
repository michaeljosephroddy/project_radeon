ALTER TABLE post_images
    ADD COLUMN IF NOT EXISTS original_image_url TEXT,
    ADD COLUMN IF NOT EXISTS display_image_url TEXT;

UPDATE post_images
SET
    original_image_url = COALESCE(original_image_url, image_url),
    display_image_url = COALESCE(display_image_url, image_url)
WHERE original_image_url IS NULL OR display_image_url IS NULL;

ALTER TABLE post_images
    ALTER COLUMN original_image_url SET NOT NULL,
    ALTER COLUMN display_image_url SET NOT NULL;
