package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"

	"github.com/lilce/blog-api/internal/markdown"
	"github.com/lilce/blog-api/internal/repository"
)

var ErrInvalidSettingKey = errors.New("invalid setting key")

// SettingService exposes a typed view of the site_settings key-value table and
// handles rendering About body_md → body_html lazily.
type SettingService struct {
	repo *repository.SettingRepository
}

func NewSettingService(repo *repository.SettingRepository) *SettingService {
	return &SettingService{repo: repo}
}

// Default keys known to the app. Any other key PUT by admin is still persisted
// but won't be surfaced by the typed GetPublic/GetAdmin shape.
const (
	KeyBrandName          = "brand.name"
	KeyBrandTagline       = "brand.tagline"
	KeyFooterText         = "footer.text"
	KeyContactEmail       = "contact.email"
	KeyContactGithub      = "contact.github"
	KeyAboutHeroTitle     = "about.hero_title"
	KeyAboutBodyMd        = "about.body_md"
	KeyNowHeroTitle       = "now.hero_title"
	KeyNowBodyMd          = "now.body_md"
	KeyUsesHeroTitle      = "uses.hero_title"
	KeyUsesBodyMd         = "uses.body_md"
	KeyColophonHeroTitle  = "colophon.hero_title"
	KeyColophonBodyMd     = "colophon.body_md"
	KeySeoSiteTitle       = "seo.site_title"
	KeySeoSiteDescription = "seo.site_description"
	KeyThemeAccent        = "theme.accent"
	KeyThemeAccentDark    = "theme.accent_dark"
)

var knownKeys = map[string]struct{}{
	KeyBrandName:          {},
	KeyBrandTagline:       {},
	KeyFooterText:         {},
	KeyContactEmail:       {},
	KeyContactGithub:      {},
	KeyAboutHeroTitle:     {},
	KeyAboutBodyMd:        {},
	KeyNowHeroTitle:       {},
	KeyNowBodyMd:          {},
	KeyUsesHeroTitle:      {},
	KeyUsesBodyMd:         {},
	KeyColophonHeroTitle:  {},
	KeyColophonBodyMd:     {},
	KeySeoSiteTitle:       {},
	KeySeoSiteDescription: {},
	KeyThemeAccent:        {},
	KeyThemeAccentDark:    {},
}

var hexColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// Public is the shape served to anonymous readers on /api/settings.
type Public struct {
	Brand    Brand   `json:"brand"`
	Footer   Footer  `json:"footer"`
	Contact  Contact `json:"contact"`
	SEO      SEO     `json:"seo"`
	About    About   `json:"about"`
	Now      About   `json:"now"`
	Uses     About   `json:"uses"`
	Colophon About   `json:"colophon"`
	Theme    Theme   `json:"theme"`
}

type Admin struct {
	Public
	AboutBodyMd    string `json:"aboutBodyMd"`
	NowBodyMd      string `json:"nowBodyMd"`
	UsesBodyMd     string `json:"usesBodyMd"`
	ColophonBodyMd string `json:"colophonBodyMd"`
}

type Brand struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}

type Footer struct {
	Text string `json:"text"`
}

type Contact struct {
	Email  string `json:"email"`
	Github string `json:"github"`
}

type SEO struct {
	SiteTitle       string `json:"siteTitle"`
	SiteDescription string `json:"siteDescription"`
}

type About struct {
	HeroTitle string `json:"heroTitle"`
	BodyHTML  string `json:"bodyHtml"`
}

type Theme struct {
	Accent     string `json:"accent"`
	AccentDark string `json:"accentDark"`
}

// EnsureDefaults seeds keys that don't yet exist so a fresh DB boots with sensible copy.
func (s *SettingService) EnsureDefaults() error {
	defaults := map[string]string{
		KeyBrandName:          "Kiri",
		KeyBrandTagline:       "· notes & essays",
		KeyFooterText:         "Written in a quiet corner of the internet · 独立写作",
		KeyContactEmail:       "hello@example.com",
		KeyContactGithub:      "https://github.com/kiri",
		KeyAboutHeroTitle:     "Hello, I'm Kiri.",
		KeyAboutBodyMd:        defaultAboutMd,
		KeyNowHeroTitle:       "What I'm doing now.",
		KeyNowBodyMd:          defaultNowMd,
		KeyUsesHeroTitle:      "What I use.",
		KeyUsesBodyMd:         defaultUsesMd,
		KeyColophonHeroTitle:  "About this site.",
		KeyColophonBodyMd:     defaultColophonMd,
		KeySeoSiteTitle:       "Kiri · Notes",
		KeySeoSiteDescription: "A personal journal on software, systems, and the craft of writing.",
		KeyThemeAccent:        "#9a2e20",
		KeyThemeAccentDark:    "#d8715e",
	}
	for k, v := range defaults {
		encoded, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err := s.repo.UpsertIfAbsent(k, string(encoded)); err != nil {
			return err
		}
	}
	return nil
}

const defaultAboutMd = `I build software for a living and write about it for the rest of my life.

这里是我写作的一隅。白天我写代码 —— Go、TypeScript、分布式系统,偶尔碰一点前端。晚上我来这里把一些想清楚了 —— 或者还没想清楚 —— 的东西写下来。

This blog is built with [Next.js 16](https://nextjs.org), [Go](https://go.dev), MySQL and Redis. No CMS, no template.

## Writing, slowly.

*Nothing is published in a hurry.* 文章会隔几天甚至几周才有新的。想订阅就收藏一下 [RSS](/feed.xml),或者直接发邮件给我。
`

const defaultNowMd = `> Inspired by [/now](https://nownownow.com/about) — a snapshot of what I'm focused on these days, refreshed every few weeks.

## Writing
- 在写一篇关于 Go 错误处理的长文
- 整理今年读到过的几本好书

## Reading
- 《On Writing Well》— William Zinsser
- 一些 distributed systems 论文

## Building
- 这个博客本身(it shows up here when I add a feature)
`

const defaultUsesMd = `## Hardware
- **Laptop** — 一台不太新的 ThinkPad
- **Keyboard** — HHKB Professional Hybrid
- **Display** — 一块够用的 27" 4K

## Editor & shell
- **Editor** — Neovim + LazyVim
- **Shell** — fish + starship
- **Terminal** — Wezterm

## Fonts
- **Sans** — Geist
- **Display serif** — Fraunces
- **Reading serif** — Source Serif 4
- **Mono** — Geist Mono

## On the web
- **Browser** — Firefox(daily) / Chromium(work)
- **Search** — Kagi
`

const defaultColophonMd = `这个站点是手搓的,不用 CMS,不用模板。

## Stack
- **Frontend** — [Next.js 16](https://nextjs.org)(App Router)+ Tailwind CSS 4 + shadcn/ui
- **Backend** — Go + Gin + GORM + MySQL 5.7 + Redis
- **Markdown** — goldmark + bluemonday(sanitize),Shiki(代码高亮)

## Design notes
- 主色 **spine red**(#9a2e20)取自旧书脊,搭奶油纸色 background
- Display serif 用 [Fraunces](https://fonts.google.com/specimen/Fraunces),正文用 [Source Serif 4](https://fonts.google.com/specimen/Source+Serif+4)
- 几乎所有动效都是纯 CSS:scroll-timeline 阅读进度、view-timeline 段落入场、view transitions 跨页过渡
- 一切动效在 ` + "`prefers-reduced-motion`" + ` 下静默

## Source
代码全部开源,欢迎围观。
`

func (s *SettingService) load() (map[string]string, error) {
	items, err := s.repo.All()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(items))
	for _, it := range items {
		var v string
		if err := json.Unmarshal([]byte(it.Value), &v); err != nil {
			// Fallback: if the stored value is not a JSON string (e.g. an object),
			// keep the raw JSON — future enhancement for complex values.
			v = it.Value
		}
		out[it.Key] = v
	}
	return out, nil
}

// GetPublic returns the site-wide public settings; AboutBodyHtml is rendered from body_md.
func (s *SettingService) GetPublic() (*Public, error) {
	vals, err := s.load()
	if err != nil {
		return nil, err
	}
	return s.buildPublic(vals), nil
}

func (s *SettingService) buildPublic(vals map[string]string) *Public {
	render := func(key string) string {
		h, err := markdown.Render(vals[key])
		if err != nil {
			return ""
		}
		return h
	}
	return &Public{
		Brand: Brand{
			Name:    vals[KeyBrandName],
			Tagline: vals[KeyBrandTagline],
		},
		Footer: Footer{Text: vals[KeyFooterText]},
		Contact: Contact{
			Email:  vals[KeyContactEmail],
			Github: vals[KeyContactGithub],
		},
		SEO: SEO{
			SiteTitle:       vals[KeySeoSiteTitle],
			SiteDescription: vals[KeySeoSiteDescription],
		},
		About: About{
			HeroTitle: vals[KeyAboutHeroTitle],
			BodyHTML:  render(KeyAboutBodyMd),
		},
		Now: About{
			HeroTitle: vals[KeyNowHeroTitle],
			BodyHTML:  render(KeyNowBodyMd),
		},
		Uses: About{
			HeroTitle: vals[KeyUsesHeroTitle],
			BodyHTML:  render(KeyUsesBodyMd),
		},
		Colophon: About{
			HeroTitle: vals[KeyColophonHeroTitle],
			BodyHTML:  render(KeyColophonBodyMd),
		},
		Theme: Theme{
			Accent:     vals[KeyThemeAccent],
			AccentDark: vals[KeyThemeAccentDark],
		},
	}
}

// GetAdmin is GetPublic + raw markdown source, in a single DB round-trip.
func (s *SettingService) GetAdmin() (*Admin, error) {
	vals, err := s.load()
	if err != nil {
		return nil, err
	}
	return &Admin{
		Public:         *s.buildPublic(vals),
		AboutBodyMd:    vals[KeyAboutBodyMd],
		NowBodyMd:      vals[KeyNowBodyMd],
		UsesBodyMd:     vals[KeyUsesBodyMd],
		ColophonBodyMd: vals[KeyColophonBodyMd],
	}, nil
}

// Update accepts { key: stringValue } map and upserts each.
// Unknown keys are rejected to prevent accidental garbage.
func (s *SettingService) Update(updates map[string]string) error {
	for k, v := range updates {
		if _, ok := knownKeys[k]; !ok {
			return ErrInvalidSettingKey
		}
		if err := validateSettingValue(k, v); err != nil {
			return err
		}
	}
	for k, v := range updates {
		encoded, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err := s.repo.Upsert(k, string(encoded)); err != nil {
			return err
		}
	}
	return nil
}

func validateSettingValue(key, value string) error {
	limits := map[string]int{
		KeyBrandName:          64,
		KeyBrandTagline:       128,
		KeyFooterText:         200,
		KeyContactEmail:       128,
		KeyContactGithub:      200,
		KeyAboutHeroTitle:     128,
		KeyAboutBodyMd:        20000,
		KeyNowHeroTitle:       128,
		KeyNowBodyMd:          20000,
		KeyUsesHeroTitle:      128,
		KeyUsesBodyMd:         20000,
		KeyColophonHeroTitle:  128,
		KeyColophonBodyMd:     20000,
		KeySeoSiteTitle:       128,
		KeySeoSiteDescription: 320,
		KeyThemeAccent:        24,
		KeyThemeAccentDark:    24,
	}
	if max, ok := limits[key]; ok && len(value) > max {
		return fmt.Errorf("%s is too long", key)
	}
	switch key {
	case KeyContactEmail:
		if value != "" {
			if _, err := mail.ParseAddress(value); err != nil {
				return fmt.Errorf("%s must be a valid email address", key)
			}
		}
	case KeyContactGithub:
		if value != "" {
			u, err := url.ParseRequestURI(value)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("%s must be a valid http(s) URL", key)
			}
		}
	case KeyThemeAccent, KeyThemeAccentDark:
		if !hexColorRe.MatchString(value) {
			return fmt.Errorf("%s must be a hex color", key)
		}
	}
	return nil
}

// BrandName is a convenience accessor used by the comment admin-reply flow.
func (s *SettingService) BrandName() (string, error) {
	v, err := s.repo.Get(KeyBrandName)
	if err != nil || v == nil {
		return "Author", err
	}
	var name string
	if err := json.Unmarshal([]byte(v.Value), &name); err != nil || name == "" {
		return "Author", nil
	}
	return name, nil
}
