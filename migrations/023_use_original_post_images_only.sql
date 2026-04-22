UPDATE post_images
SET image_url = COALESCE(original_image_url, image_url)
WHERE image_url IS DISTINCT FROM COALESCE(original_image_url, image_url);

ALTER TABLE post_images
    DROP COLUMN IF EXISTS original_image_url,
    DROP COLUMN IF EXISTS display_image_url;
