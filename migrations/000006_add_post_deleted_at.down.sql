DROP INDEX `idx_posts_deleted_at` ON `posts`;
ALTER TABLE `posts` DROP COLUMN `deleted_at`;
