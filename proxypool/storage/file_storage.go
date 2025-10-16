package storage

import (
	"bufio"
	"fmt"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/proxypool/model"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	delimiter = "|"
	numFields = 12 // ID|IP|Port|Source|Country|ScrapedProtocol|VerifiedProtocol|Latency|LastChecked|NextChecked|FailureCount|SuccessCount
)

// Storage 接口定义了代理数据持久化的行为。
type Storage interface {
	Load() (map[string]*model.ProxyInfo, error)
	Save(proxies map[string]*model.ProxyInfo) error
}

// FileStorage 实现了 Storage 接口，使用纯文本文件进行持久化。
type FileStorage struct {
	filePath string
	mu       sync.RWMutex
}

// NewFileStorage 创建一个新的 FileStorage 实例。
func NewFileStorage(filePath string) *FileStorage {
	return &FileStorage{
		filePath: filePath,
	}
}

// Load 从纯文本文件加载代理数据到内存 map 中。
func (fs *FileStorage) Load() (map[string]*model.ProxyInfo, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	l := logger.WithComponent("ProxyPool/Storage")

	file, err := os.Open(fs.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			l.Info().Str("path", fs.filePath).Msg("Proxy data file not found, starting with an empty pool.")
			return make(map[string]*model.ProxyInfo), nil
		}
		return nil, err
	}
	defer file.Close()

	proxyMap := make(map[string]*model.ProxyInfo)
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		fields := strings.Split(line, delimiter)
		if len(fields) != numFields {
			l.Warn().Int("line", lineNum).Int("expected", numFields).Int("got", len(fields)).Msg("Skipping malformed line in proxy file.")
			continue
		}

		p, err := parseProxyInfo(fields)
		if err != nil {
			l.Warn().Int("line", lineNum).Err(err).Msg("Failed to parse proxy info from line, skipping.")
			continue
		}
		proxyMap[p.ID] = p
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	l.Info().Int("count", len(proxyMap)).Msg("Successfully loaded proxies from file.")
	return proxyMap, nil
}

// Save 将内存中的代理 map 持久化到纯文本文件。
func (fs *FileStorage) Save(proxies map[string]*model.ProxyInfo) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	l := logger.WithComponent("ProxyPool/Storage")

	// 将 map 转换为 slice 以进行排序和写入。
	proxyList := make([]*model.ProxyInfo, 0, len(proxies))
	for _, p := range proxies {
		proxyList = append(proxyList, p)
	}

	sort.Slice(proxyList, func(i, j int) bool {
		return proxyList[i].ID < proxyList[j].ID
	})

	var sb strings.Builder
	for _, p := range proxyList {
		sb.WriteString(formatProxyInfo(p))
		sb.WriteString("\n")
	}

	if err := os.WriteFile(fs.filePath, []byte(sb.String()), 0644); err != nil {
		return err
	}

	l.Info().Int("count", len(proxyList)).Msg("Successfully saved proxies to file.")
	return nil
}

// formatProxyInfo 将 ProxyInfo 对象格式化为一行文本。
func formatProxyInfo(p *model.ProxyInfo) string {
	return strings.Join([]string{
		p.ID,
		p.IP,
		strconv.Itoa(p.Port),
		p.Source,
		p.Country, // Scraper is responsible for cleaning this field.
		p.ScrapedProtocol,
		p.VerifiedProtocol,
		strconv.FormatInt(p.Latency.Milliseconds(), 10),
		strconv.FormatInt(p.LastChecked.Unix(), 10),
		strconv.FormatInt(p.NextChecked.Unix(), 10),
		strconv.Itoa(p.FailureCount),
		strconv.Itoa(p.SuccessCount),
	}, delimiter)
}

// parseProxyInfo 从字符串切片解析出一个 ProxyInfo 对象。
func parseProxyInfo(fields []string) (*model.ProxyInfo, error) {
	port, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	latencyMs, err := strconv.ParseInt(fields[7], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid latency: %w", err)
	}

	lastCheckedUnix, err := strconv.ParseInt(fields[8], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid last_checked: %w", err)
	}

	nextCheckedUnix, err := strconv.ParseInt(fields[9], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid next_checked: %w", err)
	}

	failureCount, err := strconv.Atoi(fields[10])
	if err != nil {
		return nil, fmt.Errorf("invalid failure_count: %w", err)
	}

	successCount, err := strconv.Atoi(fields[11])
	if err != nil {
		return nil, fmt.Errorf("invalid success_count: %w", err)
	}

	p := &model.ProxyInfo{
		ID:               fields[0],
		IP:               fields[1],
		Port:             port,
		Source:           fields[3],
		Country:          fields[4],
		ScrapedProtocol:  fields[5],
		VerifiedProtocol: fields[6],
		Latency:          time.Duration(latencyMs) * time.Millisecond,
		FailureCount:     failureCount,
		SuccessCount:     successCount,
	}

	if lastCheckedUnix > 0 {
		p.LastChecked = time.Unix(lastCheckedUnix, 0)
	}
	if nextCheckedUnix > 0 {
		p.NextChecked = time.Unix(nextCheckedUnix, 0)
	}

	return p, nil
}
