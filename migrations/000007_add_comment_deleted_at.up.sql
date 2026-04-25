ALTER TABLE `comments` ADD COLUMN `deleted_at` TIMESTAMP NULL DEFAULT NULL;
CREATE INDEX `idx_comments_deleted_at` ON `comments` (`deleted_at`);
