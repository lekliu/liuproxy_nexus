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

// ProxydbScraper 实现了 Scraper 接口，用于抓取 proxydb.net 的免费代理。
type ProxydbScraper struct {
	client *http.Client
}

// NewProxydbScraper 创建一个新的 ProxydbScraper 实例。
func NewProxydbScraper() Scraper {
	return &ProxydbScraper{
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Name 返回抓取器的名称。
func (s *ProxydbScraper) Name() string {
	return "proxydb.net"
}

// Scrape 执行抓取操作。
func (s *ProxydbScraper) Scrape() ([]*model.ProxyInfo, error) {
	l := logger.WithComponent("ProxyPool/Scraper")
	l.Info().Str("source", s.Name()).Msg("Starting scrape...")

	var proxies []*model.ProxyInfo
	// proxydb.net 的分页是通过 offset 参数控制的，每次递增 15
	for offset := 0; offset <= 30; offset += 15 { // 先抓取前3页
		url := fmt.Sprintf("https://proxydb.net/?country=CN&protocol=http&protocol=https&protocol=socks5&offset=%d", offset)
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

		doc.Find("tbody tr").Each(func(j int, sel *goquery.Selection) {
			// Extract IP from the first <td>
			ip := strings.TrimSpace(sel.Find("td").Eq(0).Find("a").Text())

			// Extract Port from the second <td>
			portStr := strings.TrimSpace(sel.Find("td").Eq(1).Find("a").Text())

			if ip == "" || portStr == "" {
				return // Skip if essential data is missing
			}

			// Extract protocol type from the third <td>
			scrapedProtocol := strings.ToLower(strings.TrimSpace(sel.Find("td").Eq(2).Text()))

			port, err := strconv.Atoi(portStr)
			if err != nil {
				l.Warn().Str("ip", ip).Str("port", portStr).Str("source", s.Name()).Msg("Failed to parse port, skipping row.")
				return
			}

			var idSuffix string
			if scrapedProtocol == "socks5" {
				idSuffix = "-S"
			} else {
				idSuffix = "-H" // Default to HTTP for "http" and "https"
			}

			proxy := &model.ProxyInfo{
				ID:              fmt.Sprintf("%s:%d%s", ip, port, idSuffix),
				IP:              ip,
				Port:            port,
				Source:          s.Name(),
				Country:         s.Name(),
				ScrapedProtocol: scrapedProtocol,
				LastChecked:     time.Now(),
				NextChecked:     time.Now(),
			}
			proxies = append(proxies, proxy)
		})

		time.Sleep(2 * time.Second) // 友好抓取
	}

	l.Info().Int("count", len(proxies)).Str("source", s.Name()).Msg("Scrape finished.")
	return proxies, nil
}
