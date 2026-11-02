-- Post images and author identity for public reading layouts.

ALTER TABLE posts ADD COLUMN IF NOT EXISTS image_urls text[] NOT NULL DEFAULT '{}';

ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_content_check;
ALTER TABLE posts ADD CONSTRAINT posts_content_check CHECK (
    char_length(content) BETWEEN 0 AND 8000
    AND (char_length(content) > 0 OR cardinality(image_urls) > 0)
);

ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_image_urls_limit;
ALTER TABLE posts ADD CONSTRAINT posts_image_urls_limit CHECK (cardinality(image_urls) <= 4);