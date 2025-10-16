// *********** 1/1 REPLACEMENT (FINAL FIX) ***********
// --- START OF REPLACEMENT for liuproxy_nexus/proxypool/scraper/zdaye.go ---
package scraper

import (
	"fmt"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/proxypool/model"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// ZdayeScraper 实现了 Scraper 接口。
// 新版本通过一个已知的 HTTP 代理来访问目标，以绕过 WAF，并修正了解析逻辑。
type ZdayeScraper struct {
	client *http.Client
}

// NewZdayeScraper 创建一个新的 ZdayeScraper 实例。
func NewZdayeScraper(proxyURLStr string) Scraper {
	l := logger.WithComponent("ProxyPool/Scraper")
	transport := &http.Transport{}

	// 如果配置了代理 URL，则使用它
	if proxyURLStr != "" {
		proxyURL, err := url.Parse(proxyURLStr)
		if err != nil {
			l.Error().Err(err).Str("proxy_url", proxyURLStr).Msg("Invalid proxy URL for zdaye.com, falling back to direct connection.")
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			l.Info().Str("proxy_url", proxyURLStr).Msg("zdaye.com scraper will use a forward proxy.")
		}
	} else {
		l.Info().Msg("zdaye.com scraper will connect directly (no proxy).")
	}

	return &ZdayeScraper{
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

// Name 返回抓取器的名称。
func (s *ZdayeScraper) Name() string {
	return "zdaye.com"
}

// Scrape 执行抓取操作。
func (s *ZdayeScraper) Scrape() ([]*model.ProxyInfo, error) {
	l := logger.WithComponent("ProxyPool/Scraper")
	l.Info().Str("source", s.Name()).Msg("Starting scrape via proxy...")

	var proxies []*model.ProxyInfo
	// zdaye.com 的分页是 /free/1/, /free/2/ ...
	for i := 1; i <= 1; i++ {
		targetUrl := fmt.Sprintf("https://www.zdaye.com/free/%d/?https=1", i)
		l.Debug().Str("url", targetUrl).Str("source", s.Name()).Msg("Scraping page...")

		req, err := http.NewRequest("GET", targetUrl, nil)
		if err != nil {
			l.Warn().Err(err).Str("url", targetUrl).Msg("Failed to create request.")
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")

		resp, err := s.client.Do(req)
		if err != nil {
			l.Warn().Err(err).Str("url", targetUrl).Msg("Failed to fetch page via proxy.")
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			l.Warn().Int("status_code", resp.StatusCode).Str("url", targetUrl).Msg("Received non-200 status code.")
			continue
		}

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			l.Warn().Err(err).Str("url", targetUrl).Msg("Failed to parse HTML document.")
			continue
		}

		// --- 核心修正：使用正确的选择器和字段索引 ---
		doc.Find("table#ipc tbody tr").Each(func(j int, sel *goquery.Selection) {
			ip := strings.TrimSpace(sel.Find("td").Eq(0).Text())
			portStr := strings.TrimSpace(sel.Find("td").Eq(1).Text())

			proxyType := strings.TrimSpace(sel.Find("td").Eq(2).Text())

			if ip == "" || portStr == "" {
				return
			}

			port, err := strconv.Atoi(portStr)
			if err != nil {
				l.Warn().Str("ip", ip).Str("port", portStr).Msg("Failed to parse port, skipping row.")
				return
			}
			scrapedProtocol := strings.ToLower(proxyType)
			var idSuffix string
			if scrapedProtocol == "socks5" {
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
