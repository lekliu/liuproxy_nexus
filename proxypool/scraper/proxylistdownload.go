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

// ProxyListDownloadScraper 实现了 Scraper 接口
type ProxyListDownloadScraper struct {
	client *http.Client
}

// NewProxyListDownloadScraper 创建一个新的实例
func NewProxyListDownloadScraper() Scraper {
	return &ProxyListDownloadScraper{
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (s *ProxyListDownloadScraper) Name() string {
	return "proxy-list.download"
}

func (s *ProxyListDownloadScraper) Scrape() ([]*model.ProxyInfo, error) {
	l := logger.WithComponent("ProxyPool/Scraper")
	l.Info().Str("source", s.Name()).Msg("Starting scrape...")

	var proxies []*model.ProxyInfo
	url := "https://www.proxy-list.download/HTTP?country=CN"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", s.Name(), err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page for %s: %w", s.Name(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("received non-200 status code (%d) from %s", resp.StatusCode, s.Name())
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML for %s: %w", s.Name(), err)
	}

	doc.Find("table#example1 tbody#tabli tr").Each(func(j int, sel *goquery.Selection) {
		ip := strings.TrimSpace(sel.Find("td").Eq(0).Text())
		portStr := strings.TrimSpace(sel.Find("td").Eq(1).Text())

		if ip == "" || portStr == "" {
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			l.Warn().Str("ip", ip).Str("port", portStr).Msg("Failed to parse port, skipping.")
			return
		}

		// This source is for HTTP proxies
		proxy := &model.ProxyInfo{
			ID:              fmt.Sprintf("%s:%d-H", ip, port),
			IP:              ip,
			Port:            port,
			Source:          s.Name(),
			Country:         s.Name(),
			ScrapedProtocol: "http",
			LastChecked:     time.Now(),
			NextChecked:     time.Now(),
		}
		proxies = append(proxies, proxy)
	})

	l.Info().Int("count", len(proxies)).Str("source", s.Name()).Msg("Scrape finished.")
	return proxies, nil
}
