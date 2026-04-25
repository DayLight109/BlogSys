DROP INDEX `idx_comments_deleted_at` ON `comments`;
ALTER TABLE `comments` DROP COLUMN `deleted_at`;
