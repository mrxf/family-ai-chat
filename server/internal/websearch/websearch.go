// Package websearch provides pluggable live web search and page-fetching
// backends (starting with DeepSeek Search/Fetch API, with a clean driver
// interface for future engines such as SerpAPI / Serper / Tavily).
package websearch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"nuan/internal/store"
)

const (
	KindDeepSeek = "deepseek"
	KindSerp     = "serp"

	SettingActiveProvider = "search_provider"
)

var (
	ErrDisabled         = errors.New("服务器还没有开启联网搜索")
	ErrBadConfig        = errors.New("联网搜索配置不完整")
	ErrNotImplemented   = errors.New("该搜索服务暂未开放启用")
	ErrFetchUnsupported = errors.New("当前搜索服务不支持直接读取网页正文")
)

// SearchRequest is a normalized web search query.
type SearchRequest struct {
	Query            string
	Count            int
	OriginalQuestion string
	Localization     string
	NeedContent      bool
}

// SearchResult is one ranked web result returned by a search engine.
type SearchResult struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Snippet     string  `json:"snippet"`
	Summary     string  `json:"summary"`
	Content     string  `json:"content,omitempty"`
	SiteName    string  `json:"site_name,omitempty"`
	SiteIcon    string  `json:"site_icon,omitempty"`
	Date        string  `json:"date,omitempty"`
	RerankScore float64 `json:"rerank_score"`
	Authority   int     `json:"authority"`
}

// SearchResponse holds normalized search hits and estimated USD cost.
type SearchResponse struct {
	Results []SearchResult
	CostUSD float64
}

// FetchResponse holds Markdown content extracted from a single web page.
type FetchResponse struct {
	Title   string
	Content string
	CostUSD float64
}

// APIError represents an upstream HTTP error from a search or fetch API.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("search upstream %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("search upstream %d", e.Status)
}

// Retryable follows read-only search retry semantics: 408, 429 and 5xx may be retried;
// 402 (insufficient balance) and other 4xx errors must not be retried unmodified.
func (e *APIError) Retryable() bool {
	return e.Status == http.StatusRequestTimeout || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// FriendlyMessage translates upstream errors into clear Chinese messages.
func FriendlyError(err error, isFetch bool) string {
	if err == nil {
		return ""
	}
	var ae *APIError
	if errors.As(err, &ae) {
		low := strings.ToLower(ae.Message)
		switch {
		case ae.Status == 401:
			return "搜索服务 API Key 无效或已失效，需要管理员检查"
		case ae.Status == 402 || strings.Contains(low, "insufficient") || strings.Contains(low, "balance"):
			return "搜索服务账号余额不足，需要管理员充值"
		case ae.Status == 403 && isFetch:
			return "目标网站的安全策略拒绝了访问，无法直接抓取正文"
		case ae.Status == 404 && isFetch:
			return "无法读取该网页的正文（链接失效、被站点拦截或页面无可提取正文）"
		case ae.Status == 429:
			return "搜索请求太频繁，触发了限流，请稍后再试"
		case ae.Status == 408 || ae.Status == 504:
			return "搜索服务请求超时"
		case ae.Status >= 500:
			return "搜索服务暂时异常，请稍后再试"
		case ae.Message != "":
			return "搜索请求不合法：" + ae.Message
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if isFetch {
			return "读取网页超时了"
		}
		return "联网搜索超时了"
	}
	if errors.Is(err, ErrDisabled) || errors.Is(err, ErrBadConfig) || errors.Is(err, ErrNotImplemented) || errors.Is(err, ErrFetchUnsupported) {
		return err.Error()
	}
	return err.Error()
}

// Driver is implemented by every search backend (DeepSeek, SerpAPI, ...).
type Driver interface {
	Kind() string
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
	Test(ctx context.Context) (resultCount int, err error)
}

// FetchDriver is optionally implemented by backends that also fetch single URLs as Markdown.
type FetchDriver interface {
	Fetch(ctx context.Context, targetURL string) (*FetchResponse, error)
}

// DriverFactory builds a Driver for a stored SearchConfig.
type DriverFactory func(svc *Service, cfg *store.SearchConfig) (Driver, error)

// ProviderPreset describes a supported (or reserved) search provider for the admin UI.
type ProviderPreset struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Available     bool   `json:"available"`
	SupportsFetch bool   `json:"supports_fetch"`
	KeyURL        string `json:"key_url"`
	KeyPrefix     string `json:"key_prefix,omitempty"`
	DefaultURL    string `json:"default_url"`
	PriceNote     string `json:"price_note,omitempty"`
}

var Presets = []ProviderPreset{
	{
		Kind:          KindDeepSeek,
		Name:          "DeepSeek 联网搜索",
		Description:   "DeepSeek 自研中文与全球搜索引擎，支持实时资讯检索与网页正文阅读，结果准确、延迟低",
		Available:     true,
		SupportsFetch: true,
		KeyURL:        "https://platform.deepseek.com/api_keys",
		KeyPrefix:     "sk-",
		DefaultURL:    "https://api.deepseek.com",
		PriceNote:     "官方按次计费：Search / Fetch 均为 5 元 / 千次有效请求，无结果或失败不计费",
	},
	{
		Kind:          KindSerp,
		Name:          "SerpAPI (Google 搜索)",
		Description:   "Google 全球网页与新闻搜索接口（接口已预留，后续版本支持）",
		Available:     false,
		SupportsFetch: false,
		KeyURL:        "https://serpapi.com/manage-api-key",
		DefaultURL:    "https://serpapi.com",
	},
}

func PresetFor(kind string) (ProviderPreset, bool) {
	for _, p := range Presets {
		if p.Kind == kind {
			return p, true
		}
	}
	return ProviderPreset{}, false
}

var transport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          32,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 25 * time.Second,
}

// Service manages active search provider state, connection testing, and tool execution.
type Service struct {
	Store      *store.Store
	HTTPClient *http.Client

	mu      sync.RWMutex
	drivers map[string]DriverFactory
}

func NewService(st *store.Store) *Service {
	s := &Service{
		Store:      st,
		HTTPClient: &http.Client{Transport: transport, Timeout: 25 * time.Second},
		drivers:    map[string]DriverFactory{},
	}
	s.RegisterDriver(KindDeepSeek, newDeepSeekDriver)
	return s
}

// RegisterDriver registers (or overrides) a search driver factory by kind.
func (s *Service) RegisterDriver(kind string, fn DriverFactory) {
	s.mu.Lock()
	s.drivers[kind] = fn
	s.mu.Unlock()
}

// IsReady reports whether c has all required credentials filled in for its kind.
func IsReady(c *store.SearchConfig) bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.APIKey) != ""
}

// ActiveStatus returns whether web search is enabled, whether URL fetching is supported, and the active kind.
func (s *Service) ActiveStatus(ctx context.Context) (enabled bool, supportsFetch bool, kind string) {
	if s == nil || s.Store == nil {
		return false, false, ""
	}
	active, err := s.Store.GetSetting(ctx, SettingActiveProvider)
	if err != nil || strings.TrimSpace(active) == "" {
		return false, false, ""
	}
	s.mu.RLock()
	_, hasDriver := s.drivers[active]
	s.mu.RUnlock()
	pr, hasPreset := PresetFor(active)
	if hasPreset && !pr.Available && !hasDriver {
		return false, false, ""
	}
	cfg, err := s.Store.GetSearchConfig(ctx, active)
	if err != nil || !IsReady(cfg) {
		return false, false, ""
	}
	drv, err := s.driverFor(cfg)
	if err != nil {
		return false, false, ""
	}
	_, canFetch := drv.(FetchDriver)
	return true, canFetch, active
}

func (s *Service) driverFor(cfg *store.SearchConfig) (Driver, error) {
	if cfg == nil {
		return nil, ErrBadConfig
	}
	s.mu.RLock()
	fn := s.drivers[cfg.Kind]
	s.mu.RUnlock()
	if fn == nil {
		if pr, ok := PresetFor(cfg.Kind); ok && !pr.Available {
			return nil, ErrNotImplemented
		}
		return nil, ErrBadConfig
	}
	return fn(s, cfg)
}

func (s *Service) activeDriver(ctx context.Context) (Driver, error) {
	enabled, _, kind := s.ActiveStatus(ctx)
	if !enabled {
		return nil, ErrDisabled
	}
	cfg, err := s.Store.GetSearchConfig(ctx, kind)
	if err != nil || !IsReady(cfg) {
		return nil, ErrDisabled
	}
	return s.driverFor(cfg)
}

// Search executes a web search on the currently active provider.
func (s *Service) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	drv, err := s.activeDriver(ctx)
	if err != nil {
		return nil, err
	}
	return drv.Search(ctx, req)
}

// Fetch retrieves a single web page as Markdown when supported by the active provider.
func (s *Service) Fetch(ctx context.Context, targetURL string) (*FetchResponse, error) {
	drv, err := s.activeDriver(ctx)
	if err != nil {
		return nil, err
	}
	fd, ok := drv.(FetchDriver)
	if !ok {
		return nil, ErrFetchUnsupported
	}
	return fd.Fetch(ctx, targetURL)
}

// Validate checks a candidate SearchConfig before saving or testing.
func (s *Service) Validate(cfg *store.SearchConfig) error {
	if cfg == nil {
		return ErrBadConfig
	}
	s.mu.RLock()
	_, hasDriver := s.drivers[cfg.Kind]
	s.mu.RUnlock()
	pr, ok := PresetFor(cfg.Kind)
	if !ok && !hasDriver {
		return errors.New("不支持的联网搜索类型")
	}
	if ok && !pr.Available && !hasDriver {
		return ErrNotImplemented
	}
	if strings.TrimSpace(cfg.BaseURL) == "" && ok {
		cfg.BaseURL = pr.DefaultURL
	}
	if u := strings.TrimSpace(cfg.BaseURL); u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("接口地址需要以 http:// 或 https:// 开头")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("请填写 API Key")
	}
	return nil
}

// Test verifies connectivity and credentials for a candidate SearchConfig.
func (s *Service) Test(ctx context.Context, cfg *store.SearchConfig) (latencyMs int64, resultCount int, err error) {
	if err := s.Validate(cfg); err != nil {
		return 0, 0, err
	}
	drv, err := s.driverFor(cfg)
	if err != nil {
		return 0, 0, err
	}
	start := time.Now()
	n, err := drv.Test(ctx)
	return time.Since(start).Milliseconds(), n, err
}
