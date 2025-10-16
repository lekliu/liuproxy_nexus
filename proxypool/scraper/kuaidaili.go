package scraper

import (
	"encoding/json"
	"fmt"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/proxypool/model"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

// KuaidailiScraper 实现了 Scraper 接口，用于抓取 www.kuaidaili.com 的免费代理。
type KuaidailiScraper struct {
	collector *colly.Collector
}

// tempKuaidailiProxy 定义了用于解析 JS 变量中 JSON 的临时结构体。
type tempKuaidailiProxy struct {
	IP   string `json:"ip"`
	Port string `json:"port"`
}

// NewKuaidailiScraper 创建一个新的 KuaidailiScraper 实例。
func NewKuaidailiScraper() Scraper {
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36"),
	)
	c.SetRequestTimeout(20 * time.Second)

	return &KuaidailiScraper{
		collector: c,
	}
}

// Name 返回抓取器的名称。
func (s *KuaidailiScraper) Name() string {
	return "kuaidaili.com"
}

// Scrape 执行抓取操作。
func (s *KuaidailiScraper) Scrape() ([]*model.ProxyInfo, error) {
	l := logger.WithComponent("ProxyPool/Scraper")
	l.Info().Str("source", s.Name()).Msg("Starting scrape...")

	var proxies []*model.ProxyInfo
	var scrapeErr error
	var mu sync.Mutex // 使用互斥锁来安全地追加到 proxies 切片

	re := regexp.MustCompile(`(var|let|const)\s+fpsList\s*=\s*(\[.*?\]);`)

	s.collector.OnResponse(func(r *colly.Response) {
		matches := re.FindSubmatch(r.Body)
		if len(matches) < 3 {
			l.Warn().Str("url", r.Request.URL.String()).Msg("Could not find fpsList variable in response body.")
			l.Debug().Str("response_body", string(r.Body)).Msg("Dumping HTML body for diagnostics.")
			return
		}

		jsonBody := matches[2]
		var tempList []*tempKuaidailiProxy
		if err := json.Unmarshal(jsonBody, &tempList); err != nil {
			l.Warn().Err(err).Str("url", r.Request.URL.String()).Msg("Failed to unmarshal fpsList JSON.")
			scrapeErr = err
			return
		}

		mu.Lock()
		defer mu.Unlock()

		for _, p := range tempList {
			ip := strings.TrimSpace(p.IP)
			portStr := strings.TrimSpace(p.Port)

			port, err := strconv.Atoi(portStr)
			if err != nil {
				l.Warn().Str("ip", ip).Str("port", portStr).Str("source", s.Name()).Msg("Failed to parse port, skipping.")
				continue
			}

			// This site is HTTP only, so we always use -H
			idSuffix := "-H"

			proxy := &model.ProxyInfo{
				ID:              fmt.Sprintf("%s:%d%s", ip, port, idSuffix),
				IP:              ip,
				Port:            port,
				Source:          s.Name(),
				ScrapedProtocol: "http", // This site provides HTTP/HTTPS proxies, default to http
				LastChecked:     time.Now(),
				NextChecked:     time.Now(),
			}
			proxies = append(proxies, proxy)
		}
	})

	s.collector.OnError(func(r *colly.Response, err error) {
		l.Error().Err(err).Int("status_code", r.StatusCode).Str("url", r.Request.URL.String()).Msg("Scrape request failed.")
		scrapeErr = err
	})

	// --- 核心修改：循环访问多个页面 ---
	for i := 1; i <= 2; i++ {
		url := fmt.Sprintf("https://www.kuaidaili.com/free/intr/%d/", i)
		l.Debug().Str("url", url).Msg("Visiting page...")
		s.collector.Visit(url)
		time.Sleep(2 * time.Second) // 在请求之间添加短暂延迟，避免对目标服务器造成过大压力
	}
	for i := 1; i <= 2; i++ {
		url := fmt.Sprintf("https://www.kuaidaili.com/free/inha/%d/", i)
		l.Debug().Str("url", url).Msg("Visiting page...")
		s.collector.Visit(url)
		time.Sleep(2 * time.Second) // 在请求之间添加短暂延迟，避免对目标服务器造成过大压力
	}

	s.collector.Wait() // 等待所有排队的 Visit 请求完成

	if scrapeErr != nil {
		return nil, scrapeErr
	}

	l.Info().Int("count", len(proxies)).Str("source", s.Name()).Msg("Scrape finished.")
	return proxies, nil
}
