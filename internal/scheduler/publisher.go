// Package scheduler 提供后台周期任务,目前只有把到点的 scheduled 文章翻成 published。
package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/lilce/blog-api/internal/repository"
)

// Publisher 周期性扫描 status='scheduled' AND published_at<=NOW() 的文章并翻状态。
// 默认每分钟一次:对一个个人博客来说精度足够,且 Update 走主键索引的 status 列,代价极低。
type Publisher struct {
	posts    *repository.PostRepository
	interval time.Duration
}

func NewPublisher(posts *repository.PostRepository) *Publisher {
	return &Publisher{posts: posts, interval: time.Minute}
}

// Run 阻塞直到 ctx 取消。每个 tick 调一次 PromoteScheduled。
func (p *Publisher) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()

	// 启动时立即跑一次,避免必须等够 interval 才开始第一次扫描。
	p.tick()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick()
		}
	}
}

func (p *Publisher) tick() {
	n, err := p.posts.PromoteScheduled()
	if err != nil {
		log.Printf("scheduler: promote failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("scheduler: promoted %d scheduled post(s)", n)
	}
}
