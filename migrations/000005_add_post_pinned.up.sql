ALTER TABLE `posts` ADD COLUMN `pinned` TINYINT(1) NOT NULL DEFAULT 0;
CREATE INDEX `idx_posts_pinned` ON `posts` (`pinned`);
