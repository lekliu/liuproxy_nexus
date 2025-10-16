package scraper

import "liuproxy_nexus/proxypool/model"

// Scraper 接口定义了从代理源抓取代理信息的行为。
type Scraper interface {
	// Scrape 执行抓取操作，并返回一个 ProxyInfo 切片。
	// 实现者应只负责抓取和初步解析，不进行验证。
	Scrape() ([]*model.ProxyInfo, error)

	// Name 返回抓取器的名称，用于日志记录。
	Name() string
}
