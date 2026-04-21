DROP INDEX `idx_posts_pinned` ON `posts`;
ALTER TABLE `posts` DROP COLUMN `pinned`;
