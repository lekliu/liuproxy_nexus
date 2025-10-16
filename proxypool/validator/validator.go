package validator

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/proxypool/model"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

const (
	defaultValidationTarget = "www.baidu.com:443" // Use a target that requires TLS
	geoAPITimeout           = 5 * time.Second
)

// geoAPIResponse defines the structure for the ip-api.com JSON response.
type geoAPIResponse struct {
	Status     string `json:"status"`
	Country    string `json:"country"`
	RegionName string `json:"regionName"` // Province
	City       string `json:"city"`
}

type Validator struct {
	timeout     time.Duration
	concurrency int
	geoClient   *http.Client // Add a dedicated client for Geo API calls
}

func NewValidator(timeout time.Duration, concurrency int) *Validator {
	if concurrency <= 0 {
		concurrency = 5
	}
	return &Validator{
		timeout:     timeout,
		concurrency: concurrency,
		geoClient: &http.Client{
			Timeout: geoAPITimeout,
		},
	}
}

func (v *Validator) Validate(proxies []*model.ProxyInfo) []*model.ProxyInfo {
	l := logger.WithComponent("ProxyPool/Validator")
	if len(proxies) == 0 {
		return proxies
	}

	l.Info().Int("count", len(proxies)).Int("concurrency", v.concurrency).Msg("Starting validation batch...")

	var wg sync.WaitGroup
	resultsChan := make(chan *model.ProxyInfo, len(proxies))
	semaphore := make(chan struct{}, v.concurrency)

	for _, p := range proxies {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(proxy *model.ProxyInfo) {
			defer wg.Done()
			defer func() { <-semaphore }()

			v.validateSingleProxy(proxy)
			resultsChan <- proxy
		}(p)
	}

	wg.Wait()
	close(resultsChan)

	validatedProxies := make([]*model.ProxyInfo, 0, len(proxies))
	for p := range resultsChan {
		validatedProxies = append(validatedProxies, p)
	}

	l.Info().Msg("Validation batch finished.")
	return validatedProxies
}

// validateSingleProxy acts as a dispatcher based on the scraped protocol.
func (v *Validator) validateSingleProxy(p *model.ProxyInfo) {
	startTime := time.Now()
	var err error

	// Reset verified protocol before each validation
	p.VerifiedProtocol = ""

	switch p.ScrapedProtocol {
	case "socks5":
		err = v.checkSocks5Connect(p)
		if err == nil {
			p.VerifiedProtocol = "socks5"
		}
	case "http", "https", "": // Default to checking for HTTP CONNECT support
		fallthrough
	default:
		err = v.checkHttpConnect(p)
		if err == nil {
			p.VerifiedProtocol = "http"
		}
	}

	latency := time.Since(startTime)
	p.LastChecked = time.Now()

	if err != nil {
		p.SuccessCount = 0
		p.FailureCount++
		p.Latency = 0
	} else {
		p.FailureCount = 0
		p.SuccessCount++
		p.Latency = latency

		// --- NEW: Fetch and format geo info on success ---
		country, region, city := v.fetchGeoInfo(p.IP)
		if country != "" {
			p.Country = formatCountryField(p, country, region, city)
		}
	}
}

// fetchGeoInfo queries the ip-api.com service.
func (v *Validator) fetchGeoInfo(ip string) (country, region, city string) {
	l := logger.WithComponent("ProxyPool/Validator")
	apiURL := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,regionName,city&lang=zh-CN", ip)

	resp, err := v.geoClient.Get(apiURL)
	if err != nil {
		l.Warn().Err(err).Str("ip", ip).Msg("Geo API request failed.")
		return "", "", ""
	}
	defer resp.Body.Close()

	var apiResp geoAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		l.Warn().Err(err).Str("ip", ip).Msg("Failed to decode Geo API response.")
		return "", "", ""
	}

	if apiResp.Status != "success" {
		l.Debug().Str("ip", ip).Str("status", apiResp.Status).Msg("Geo API returned non-success status.")
		return "", "", ""
	}

	return apiResp.Country, apiResp.RegionName, apiResp.City
}

// formatCountryField formats the country field based on the new standard.
func formatCountryField(p *model.ProxyInfo, country, region, city string) string {
	var sourceAbbr string
	switch p.Source {
	case "zdaye.com":
		sourceAbbr = "zdaye"
	case "proxy-list.download":
		sourceAbbr = "prold"
	case "proxydb.net":
		sourceAbbr = "proxy"
	case "kuaidaili.com":
		sourceAbbr = "快"
	case "qiyunip.com":
		sourceAbbr = "qiyun"
	case "ip3366.net":
		sourceAbbr = "ip336"
	case "manual-import":
		sourceAbbr = ""
	default:
		sourceAbbr = ""
	}

	if country == "中国" {
		// Clean up region and city names
		region = strings.TrimSuffix(region, " Sheng")
		region = strings.TrimSuffix(region, " Shi")
		region = strings.TrimSuffix(region, " Zizhiqu")
		city = strings.TrimSuffix(city, " Shi")
		return fmt.Sprintf("%s-%s-%s", region, city, sourceAbbr)
	}

	return fmt.Sprintf("%s-%s", country, sourceAbbr)
}

// checkHttpConnect validates a proxy by attempting an HTTP CONNECT request.
func (v *Validator) checkHttpConnect(p *model.ProxyInfo) error {
	l := logger.WithComponent("ProxyPool/Validator")
	proxyAddr := fmt.Sprintf("http://%s:%d", p.IP, p.Port)
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		l.Warn().Err(err).Str("proxy_id", p.ID).Msg("Invalid HTTP proxy URL format.")
		return err
	}

	dialer := &net.Dialer{
		Timeout:   v.timeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		IdleConnTimeout:       v.timeout,
		TLSHandshakeTimeout:   v.timeout / 2,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   v.timeout,
	}

	req, err := http.NewRequest("HEAD", "https://"+defaultValidationTarget, nil)
	if err != nil {
		l.Error().Err(err).Msg("Failed to create HEAD request.")
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("received non-successful status code: %d", resp.StatusCode)
	}

	return nil
}

// checkSocks5Connect validates a proxy by attempting a SOCKS5 connection.
func (v *Validator) checkSocks5Connect(p *model.ProxyInfo) error {
	proxyAddr := fmt.Sprintf("%s:%d", p.IP, p.Port)
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, &net.Dialer{Timeout: v.timeout})
	if err != nil {
		return fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	// Create a context with a deadline that covers the entire operation.
	ctx, cancel := context.WithTimeout(context.Background(), v.timeout)
	defer cancel()

	// Use the context-aware DialContext method.
	conn, err := dialer.(proxy.ContextDialer).DialContext(ctx, "tcp", defaultValidationTarget)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}
