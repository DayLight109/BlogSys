package database

import "gorm.io/gorm"

// EnsurePostSearchIndex creates the FULLTEXT index required by
// PostRepository.Search when a fresh database is bootstrapped by AutoMigrate
// instead of the SQL migration files.
func EnsurePostSearchIndex(db *gorm.DB) error {
	var count int64
	if err := db.Raw(`
SELECT COUNT(1)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = 'posts'
  AND index_name = 'ft_posts_title_content'
`).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return db.Exec("ALTER TABLE `posts` ADD FULLTEXT KEY `ft_posts_title_content` (`title`, `content_md`) WITH PARSER ngram").Error
}
