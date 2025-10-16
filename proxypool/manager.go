// *********** 1/1 REPLACEMENT: Implement proxy selection logic in GetAvailableProxies ***********
// --- START OF REPLACEMENT for liuproxy_nexus/proxypool/manager.go ---
package manager

import (
	"fmt"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/internal/shared/types"
	"liuproxy_nexus/proxypool/model"
	"liuproxy_nexus/proxypool/scraper"
	"liuproxy_nexus/proxypool/storage"
	"liuproxy_nexus/proxypool/validator"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// 代理因连续失败而被移除的阈值
	maxFailuresBeforeRemoval = 7
)

// 定义分级间隔策略
var (
	// 成功验证后的下一次检查间隔，与 SuccessCount 对应
	successIntervals = []time.Duration{
		1 * time.Hour,
		12 * time.Hour,
		24 * time.Hour,  // 1d
		48 * time.Hour,  // 2d
		72 * time.Hour,  // 3d
		120 * time.Hour, // 5d
	}

	// 失败验证后的指数退避间隔，与 FailureCount 对应
	failureIntervals = []time.Duration{
		1 * time.Hour,
		12 * time.Hour,
		24 * time.Hour,  // 1d
		48 * time.Hour,  // 2d
		72 * time.Hour,  // 3d
		120 * time.Hour, // 5d
	}
)

// Manager 是代理池模块的总控制器。
type Manager struct {
	cfg       *types.Config
	storage   storage.Storage
	scrapers  []scraper.Scraper
	validator *validator.Validator
	proxies   map[string]*model.ProxyInfo // 内存中的代理池
	mu        sync.RWMutex

	// 调度器与生命周期管理
	scrapeTicker      *time.Ticker
	healthCheckTicker *time.Ticker
	stopChan          chan struct{}
	wg                sync.WaitGroup
}

// NewManager 创建并初始化代理池管理器。
func NewManager(cfg *types.Config, storage storage.Storage, validator *validator.Validator) *Manager {
	m := &Manager{
		cfg:       cfg,
		storage:   storage,
		validator: validator,
		proxies:   make(map[string]*model.ProxyInfo),
		stopChan:  make(chan struct{}),
	}
	m.AddScraper(scraper.NewKuaidailiScraper())
	m.AddScraper(scraper.NewZdayeScraper(cfg.ProxyPoolConf.ZdayeProxyURL))
	m.AddScraper(scraper.NewQiyunipScraper())
	m.AddScraper(scraper.NewIP3366Scraper())
	m.AddScraper(scraper.NewProxydbScraper())
	m.AddScraper(scraper.NewProxyListDownloadScraper())

	return m
}

// AddScraper 添加一个抓取器到管理器。
func (m *Manager) AddScraper(s scraper.Scraper) {
	m.scrapers = append(m.scrapers, s)
}

// Start 启动管理器的所有后台任务（调度循环）。
func (m *Manager) Start() {
	l := logger.WithComponent("ProxyPool/Manager")
	l.Info().Msg("Manager starting...")

	if err := m.loadProxies(); err != nil {
		l.Error().Err(err).Msg("Failed to load proxies from storage. Starting with an empty pool.")
	}

	scrapeInterval := time.Duration(m.cfg.ProxyPoolConf.ScrapeIntervalHours) * time.Hour
	healthCheckInterval := time.Duration(m.cfg.ProxyPoolConf.HealthCheckIntervalSeconds) * time.Second
	m.scrapeTicker = time.NewTicker(scrapeInterval)
	m.healthCheckTicker = time.NewTicker(healthCheckInterval)

	l.Info().
		Dur("scrape_interval", scrapeInterval).
		Dur("health_check_interval", healthCheckInterval).
		Msg("Schedulers initialized.")

	m.wg.Add(1)
	go m.schedulerLoop()

	go m.runScrapeAndValidateCycle()
}

// schedulerLoop 是核心的调度循环，监听 Ticker 和停止信号。
func (m *Manager) schedulerLoop() {
	defer m.wg.Done()
	l := logger.WithComponent("ProxyPool/Manager")

	for {
		select {
		case <-m.scrapeTicker.C:
			l.Info().Msg("Scrape ticker triggered.")
			go m.runScrapeAndValidateCycle()

		case <-m.healthCheckTicker.C:
			l.Debug().Msg("Health check ticker triggered.")
			go m.runRevalidationCycle()

		case <-m.stopChan:
			l.Info().Msg("Stop signal received. Shutting down schedulers.")
			m.scrapeTicker.Stop()
			m.healthCheckTicker.Stop()
			return
		}
	}
}

// runScrapeAndValidateCycle 执行一个完整的“抓取 -> 验证新代理 -> 存储”周期。
func (m *Manager) runScrapeAndValidateCycle() {
	l := logger.WithComponent("ProxyPool/Manager")
	l.Info().Msg("Starting new scrape and validate cycle...")

	var wg sync.WaitGroup
	scrapedChan := make(chan []*model.ProxyInfo, len(m.scrapers))

	for _, s := range m.scrapers {
		wg.Add(1)
		go func(sc scraper.Scraper) {
			defer wg.Done()
			proxies, err := sc.Scrape()
			if err != nil {
				l.Warn().Err(err).Str("source", sc.Name()).Msg("Scraper failed.")
				return
			}
			if len(proxies) > 0 {
				scrapedChan <- proxies
			}
		}(s)
	}

	wg.Wait()
	close(scrapedChan)

	newProxiesToValidate := make([]*model.ProxyInfo, 0)
	m.mu.RLock()
	for proxies := range scrapedChan {
		for _, p := range proxies {
			if _, exists := m.proxies[p.ID]; !exists {
				newProxiesToValidate = append(newProxiesToValidate, p)
			}
		}
	}
	m.mu.RUnlock()

	if len(newProxiesToValidate) == 0 {
		l.Info().Msg("No new proxies found to validate in this cycle.")
		if err := m.saveProxies(); err != nil {
			l.Error().Err(err).Msg("Failed to save proxies to storage.")
		}
		return
	}

	l.Info().Int("count", len(newProxiesToValidate)).Msg("Found new proxies. Starting validation...")
	validatedProxies := m.validator.Validate(newProxiesToValidate)

	supportedConnectCount := 0
	m.mu.Lock()
	for _, p := range validatedProxies {
		m.proxies[p.ID] = p
		// 对于新代理，我们也需要设置下一次检查时间
		m.updateProxyState(p)
		if p.VerifiedProtocol != "" {
			supportedConnectCount++
		}
	}
	m.mu.Unlock()

	l.Info().Int("total_validated", len(validatedProxies)).Int("supported_connect", supportedConnectCount).Msg("Validation finished.")
	if err := m.saveProxies(); err != nil {
		l.Error().Err(err).Msg("Failed to save proxies to storage after cycle.")
	}
	l.Info().Msg("Scrape and validate cycle finished.")
}

// runRevalidationCycle 执行一个“筛选存量代理 -> 验证 -> 更新状态”的周期。
func (m *Manager) runRevalidationCycle() {
	l := logger.WithComponent("ProxyPool/Manager")
	l.Debug().Msg("Executing re-validation cycle...")

	// 1. 筛选出到期待验证的代理
	now := time.Now()
	dueProxies := make([]*model.ProxyInfo, 0)
	m.mu.RLock()
	for _, p := range m.proxies {
		if !p.NextChecked.IsZero() && p.NextChecked.Before(now) {
			dueProxies = append(dueProxies, p)
		}
	}
	m.mu.RUnlock()

	if len(dueProxies) == 0 {
		l.Debug().Msg("No proxies due for re-validation.")
		return
	}

	// 2. 排序并取出一批进行验证
	sort.Slice(dueProxies, func(i, j int) bool {
		return dueProxies[i].NextChecked.Before(dueProxies[j].NextChecked)
	})

	batchSize := m.cfg.ProxyPoolConf.RevalidationBatchSize
	if len(dueProxies) > batchSize {
		dueProxies = dueProxies[:batchSize]
	}

	l.Info().Int("batch_size", len(dueProxies)).Int("total_due", len(dueProxies)).Msg("Starting re-validation batch.")

	// 3. 验证
	validatedProxies := m.validator.Validate(dueProxies)

	// 4. 更新状态
	m.mu.Lock()
	defer m.mu.Unlock()

	var removedCount int
	for _, p := range validatedProxies {
		// 直接在 m.proxies 中的代理上更新状态
		if proxyToUpdate, ok := m.proxies[p.ID]; ok {
			m.updateProxyState(proxyToUpdate)

			// 检查是否需要淘汰
			if proxyToUpdate.FailureCount >= maxFailuresBeforeRemoval {
				delete(m.proxies, proxyToUpdate.ID)
				removedCount++
				l.Info().Str("proxy_id", proxyToUpdate.ID).Int("failures", proxyToUpdate.FailureCount).Msg("Proxy removed from pool due to excessive failures.")
			}
		}
	}

	// 5. 如果有状态变更，则保存
	if len(validatedProxies) > 0 {
		// 使用 goroutine 在后台保存，避免阻塞调度器
		go func() {
			if err := m.saveProxies(); err != nil {
				l.Error().Err(err).Msg("Failed to save proxies after re-validation cycle.")
			}
		}()
	}
}

// updateProxyState 是动态间隔算法的核心实现。
// 注意：此函数必须在写锁 (m.mu.Lock) 保护下调用。
func (m *Manager) updateProxyState(p *model.ProxyInfo) {
	now := time.Now()
	if p.VerifiedProtocol != "" {
		// 成功逻辑
		successIndex := p.SuccessCount - 1
		if successIndex < 0 {
			successIndex = 0
		}
		if successIndex >= len(successIntervals) {
			successIndex = len(successIntervals) - 1
		}
		p.NextChecked = now.Add(successIntervals[successIndex])
	} else {
		// 失败逻辑
		failureIndex := p.FailureCount - 1
		if failureIndex < 0 {
			failureIndex = 0
		}
		if failureIndex >= len(failureIntervals) {
			failureIndex = len(failureIntervals) - 1
		}
		p.NextChecked = now.Add(failureIntervals[failureIndex])
	}
}

// loadProxies 从存储加载代理到内存。
func (m *Manager) loadProxies() error {
	proxies, err := m.storage.Load()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.proxies = proxies
	m.mu.Unlock()
	return nil
}

// saveProxies 将内存中的代理保存到存储。
func (m *Manager) saveProxies() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.storage.Save(m.proxies)
}

// Stop 优雅地停止管理器的所有后台任务。
func (m *Manager) Stop() {
	close(m.stopChan)
	m.wg.Wait()
	if err := m.saveProxies(); err != nil {
		logger.Error().Err(err).Msg("Failed to save proxies on shutdown.")
	}
	logger.Info().Msg("ProxyPool Manager gracefully stopped.")
}

// GetAvailableProxies 从内存池中返回指定数量的可用代理。
func (m *Manager) GetAvailableProxies(count int) []*model.ProxyInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. 筛选出所有健康的候选代理
	candidates := make([]*model.ProxyInfo, 0)
	for _, p := range m.proxies {
		if p.VerifiedProtocol != "" && p.SuccessCount > 0 {
			candidates = append(candidates, p)
		}
	}

	// 2. 按照优选逻辑排序
	sort.Slice(candidates, func(i, j int) bool {
		// 主要排序条件：成功次数越多越好 (降序)
		if candidates[i].SuccessCount != candidates[j].SuccessCount {
			return candidates[i].SuccessCount > candidates[j].SuccessCount
		}
		// 次要排序条件：延迟越低越好 (升序)
		return candidates[i].Latency < candidates[j].Latency
	})

	// 3. 返回指定数量的最佳代理
	if len(candidates) > count {
		return candidates[:count]
	}

	return candidates
}

// ImportAndValidateProxies adds a list of proxies to the pool and triggers an immediate validation for them.
func (m *Manager) ImportAndValidateProxies(proxyStrings []string, protocol string) error {
	l := logger.WithComponent("ProxyPool/Manager")
	l.Info().Int("count", len(proxyStrings)).Str("protocol", protocol).Msg("Starting manual proxy import.")

	newProxies := make([]*model.ProxyInfo, 0)
	var idSuffix string
	if protocol == "socks5" {
		idSuffix = "-S"
	} else {
		idSuffix = "-H"
	}

	m.mu.Lock() // Lock for writing
	for _, proxyStr := range proxyStrings {
		trimmedStr := strings.TrimSpace(proxyStr)
		if trimmedStr == "" {
			continue
		}

		parts := strings.Split(trimmedStr, ":")
		if len(parts) != 2 {
			l.Warn().Str("proxy", trimmedStr).Msg("Invalid proxy format, skipping.")
			continue
		}
		ip := parts[0]
		portStr := parts[1]
		port, err := strconv.Atoi(portStr)
		if err != nil {
			l.Warn().Str("proxy", trimmedStr).Msg("Invalid port, skipping.")
			continue
		}

		id := fmt.Sprintf("%s:%d%s", ip, port, idSuffix)
		if _, exists := m.proxies[id]; exists {
			l.Debug().Str("proxy_id", id).Msg("Proxy already exists, skipping import.")
			continue
		}

		proxy := &model.ProxyInfo{
			ID:              id,
			IP:              ip,
			Port:            port,
			Source:          "manual-import",
			ScrapedProtocol: protocol,
			LastChecked:     time.Now(),
			NextChecked:     time.Now(), // Mark as due for immediate checking
		}
		m.proxies[id] = proxy
		newProxies = append(newProxies, proxy)
	}
	m.mu.Unlock()

	if len(newProxies) == 0 {
		l.Info().Msg("No new proxies were added from the import list.")
		return nil
	}

	l.Info().Int("count", len(newProxies)).Msg("New proxies added to the pool. Triggering background validation.")

	// Run validation and saving in the background
	go func() {
		validated := m.validator.Validate(newProxies)

		m.mu.Lock()
		for _, p := range validated {
			if proxyToUpdate, ok := m.proxies[p.ID]; ok {
				// Validator updates the state (Success/Failure count), so we just need to calculate the next check time.
				m.updateProxyState(proxyToUpdate)
			}
		}
		m.mu.Unlock()

		// Save all changes to disk
		if err := m.saveProxies(); err != nil {
			l.Error().Err(err).Msg("Failed to save proxies after manual import and validation.")
		} else {
			l.Info().Msg("Successfully saved proxies after manual import.")
		}
	}()

	return nil
}

// GetAllProxies returns a snapshot of all proxies currently in the pool.
func (m *Manager) GetAllProxies() []*model.ProxyInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allProxies := make([]*model.ProxyInfo, 0, len(m.proxies))
	for _, p := range m.proxies {
		allProxies = append(allProxies, p)
	}

	// Sort by last checked time for a consistent initial view
	sort.Slice(allProxies, func(i, j int) bool {
		return allProxies[i].LastChecked.After(allProxies[j].LastChecked)
	})

	return allProxies
}

// TriggerValidation schedules an immediate background validation for a specific list of proxy IDs.
func (m *Manager) TriggerValidation(ids []string) error {
	l := logger.WithComponent("ProxyPool/Manager")
	l.Info().Int("count", len(ids)).Msg("Manual validation triggered for specific proxies.")

	proxiesToValidate := make([]*model.ProxyInfo, 0, len(ids))
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	m.mu.RLock()
	for id, proxy := range m.proxies {
		if _, ok := idSet[id]; ok {
			proxiesToValidate = append(proxiesToValidate, proxy)
		}
	}
	m.mu.RUnlock()

	if len(proxiesToValidate) == 0 {
		return fmt.Errorf("no matching proxies found for the given IDs")
	}

	go func() {
		validated := m.validator.Validate(proxiesToValidate)

		m.mu.Lock()
		for _, p := range validated {
			if proxyToUpdate, ok := m.proxies[p.ID]; ok {
				m.updateProxyState(proxyToUpdate)
			}
		}
		m.mu.Unlock()

		if err := m.saveProxies(); err != nil {
			l.Error().Err(err).Msg("Failed to save proxies after manual validation.")
		}
	}()

	return nil
}

// DeleteProxies removes a list of proxies from the pool by their IDs.
func (m *Manager) DeleteProxies(ids []string) error {
	l := logger.WithComponent("ProxyPool/Manager")
	l.Info().Int("count", len(ids)).Msg("Deleting proxies from pool.")

	m.mu.Lock()
	deletedCount := 0
	for _, id := range ids {
		if _, exists := m.proxies[id]; exists {
			delete(m.proxies, id)
			deletedCount++
		}
	}
	m.mu.Unlock()

	l.Info().Int("deleted_count", deletedCount).Msg("Deletion complete.")

	// Immediately save changes to disk
	return m.saveProxies()
}
