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

// QiyunipScraper 实现了 Scraper 接口，用于抓取 www.qiyunip.com 的免费代理。
type QiyunipScraper struct {
	client *http.Client
}

// NewQiyunipScraper 创建一个新的 QiyunipScraper 实例。
func NewQiyunipScraper() Scraper {
	return &QiyunipScraper{
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Name 返回抓取器的名称。
func (s *QiyunipScraper) Name() string {
	return "qiyunip.com"
}

// Scrape 执行抓取操作。
func (s *QiyunipScraper) Scrape() ([]*model.ProxyInfo, error) {
	l := logger.WithComponent("ProxyPool/Scraper")
	l.Info().Str("source", s.Name()).Msg("Starting scrape...")

	var proxies []*model.ProxyInfo
	for i := 1; i <= 1; i++ {
		url := fmt.Sprintf("https://www.qiyunip.com/freeProxy/%d.html", i)
		l.Debug().Str("url", url).Str("source", s.Name()).Msg("Scraping page...")

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			l.Warn().Err(err).Str("url", url).Msg("Failed to create request.")
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")

		resp, err := s.client.Do(req)
		if err != nil {
			l.Warn().Err(err).Str("url", url).Msg("Failed to fetch page.")
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			l.Warn().Int("status_code", resp.StatusCode).Str("url", url).Msg("Received non-200 status code.")
			continue
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			l.Warn().Err(err).Str("url", url).Msg("Failed to parse HTML document.")
			continue
		}

		// --- 核心修正 ---
		// Use a more specific selector for the table body rows.
		doc.Find("table#proxyTable tbody tr").Each(func(j int, sel *goquery.Selection) {
			// NOTE: This site uses non-standard HTML where data cells are <th>.
			cells := sel.Find("th")
			if cells.Length() < 4 { // Ensure we have at least IP, Port, Anonymity, and Type.
				return
			}

			proxyType := strings.TrimSpace(cells.Eq(3).Text())
			// Relax the filter to include both "http" and "https" types.
			// The Validator will later determine if they truly support CONNECT.
			if !strings.Contains(strings.ToUpper(proxyType), "HTTP") {
				return
			}

			ip := strings.TrimSpace(cells.Eq(0).Text())
			portStr := strings.TrimSpace(cells.Eq(1).Text())

			port, err := strconv.Atoi(portStr)

			if err != nil || ip == "" {
				l.Warn().Str("ip", ip).Str("port", portStr).Msg("Failed to parse IP/port, skipping row.")
				return
			}

			scrapedProtocol := strings.ToLower(proxyType)
			var idSuffix string
			if scrapedProtocol == "socks5" { // Although this site is mainly http, we check just in case.
				idSuffix = "-S"
			} else {
				idSuffix = "-H"
			}

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

		time.Sleep(3 * time.Second)
	}

	l.Info().Int("count", len(proxies)).Str("source", s.Name()).Msg("Scrape finished.")
	return proxies, nil
}
