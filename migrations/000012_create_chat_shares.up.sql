CREATE TABLE `chat_shares` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `hash` VARCHAR(32) NOT NULL,
  `created_by` BIGINT UNSIGNED NOT NULL,
  `title` VARCHAR(255) NOT NULL,
  `payload` JSON NOT NULL,
  `view_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_chat_shares_hash` (`hash`),
  KEY `idx_chat_shares_created_by` (`created_by`),
  CONSTRAINT `fk_chat_shares_user` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
