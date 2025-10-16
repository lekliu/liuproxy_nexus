package scraper

import (
	"fmt"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/proxypool/model"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// IP3366Scraper 实现了 Scraper 接口，用于抓取 http://www.ip3366.net 的免费代理。
type IP3366Scraper struct {
	client *http.Client
}

// NewIP3366Scraper 创建一个新的 IP3366Scraper 实例。
func NewIP3366Scraper() Scraper {
	return &IP3366Scraper{
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Name 返回抓取器的名称。
func (s *IP3366Scraper) Name() string {
	return "ip3366.net"
}

// Scrape 执行抓取操作。
func (s *IP3366Scraper) Scrape() ([]*model.ProxyInfo, error) {
	l := logger.WithComponent("ProxyPool/Scraper")
	l.Info().Str("source", s.Name()).Msg("Starting scrape...")

	var proxies []*model.ProxyInfo
	// ip3366.net 的分页是 /?stype=1&page=1, /?stype=1&page=2 ...
	for i := 1; i <= 1; i++ {
		url := fmt.Sprintf("http://www.ip3366.net/?stype=1&page=%d", i)
		l.Debug().Str("url", url).Str("source", s.Name()).Msg("Scraping page...")

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			l.Warn().Err(err).Str("url", url).Str("source", s.Name()).Msg("Failed to create request.")
			continue
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")

		resp, err := s.client.Do(req)
		if err != nil {
			l.Warn().Err(err).Str("url", url).Str("source", s.Name()).Msg("Failed to fetch page.")
			continue
		}

		if resp.StatusCode != 200 {
			l.Warn().Int("status_code", resp.StatusCode).Str("url", url).Str("source", s.Name()).Msg("Received non-200 status code.")
			resp.Body.Close()
			continue
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if err != nil {
			l.Warn().Err(err).Str("url", url).Str("source", s.Name()).Msg("Failed to parse HTML document.")
			continue
		}

		// 表格选择器
		doc.Find("table.table-bordered tbody tr").Each(func(j int, sel *goquery.Selection) {
			ip := strings.TrimSpace(sel.Find("td").Eq(0).Text())
			portStr := strings.TrimSpace(sel.Find("td").Eq(1).Text())

			proxyType := strings.TrimSpace(sel.Find("td").Eq(3).Text())

			upperProxyType := strings.ToUpper(proxyType)
			if !strings.Contains(upperProxyType, "HTTP") {
				return
			}

			port, err := strconv.Atoi(portStr)
			if err != nil || ip == "" {
				l.Warn().Str("ip", ip).Str("port", portStr).Str("source", s.Name()).Msg("Failed to parse IP/port, skipping row.")
				return
			}

			//scrapedProtocol := strings.ToLower(proxyType)
			// This site is HTTP only, so we always use -H
			idSuffix := "-H"

			proxy := &model.ProxyInfo{
				ID:              fmt.Sprintf("%s:%d%s", ip, port, idSuffix),
				IP:              ip,
				Port:            port,
				Source:          s.Name(),
				Country:         s.Name(),
				ScrapedProtocol: strings.ToLower(proxyType),
				LastChecked:     time.Now(),
				NextChecked:     time.Now(),
			}
			proxies = append(proxies, proxy)
		})

		time.Sleep(2 * time.Second)
	}

	l.Info().Int("count", len(proxies)).Str("source", s.Name()).Msg("Scrape finished.")
	return proxies, nil
}
