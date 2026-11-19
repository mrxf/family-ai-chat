package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/store"
)

const (
	deepseekMaxQueryRunes = 128
	deepseekMaxFetchURL   = 8192
	// DeepSeek bills 5 RMB per 1,000 non-empty Search or Fetch calls (~$0.0007 USD each).
	deepseekCallCostUSD = 0.0007
)

type deepSeekDriver struct {
	svc     *Service
	baseURL string
	apiKey  string
}

func newDeepSeekDriver(svc *Service, cfg *store.SearchConfig) (Driver, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://api.deepseek.com"
	}
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, ErrBadConfig
	}
	return &deepSeekDriver{svc: svc, baseURL: base, apiKey: key}, nil
}

func (d *deepSeekDriver) Kind() string { return KindDeepSeek }

// DeepSeekEndpoint builds the full URL for DeepSeek's /v1/tools/{tool} API,
// tolerating base URLs with or without a trailing /v1 (e.g. "https://api.deepseek.com"
// and "https://api.deepseek.com/v1" both resolve to ".../v1/tools/search").
func DeepSeekEndpoint(baseURL, tool string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://api.deepseek.com"
	}
	u, err := url.Parse(base)
	if err != nil {
		return base + "/v1/tools/" + tool
	}
	switch {
	case u.Path == "":
		return base + "/v1/tools/" + tool
	case strings.HasSuffix(u.Path, "/v1/tools"):
		return base + "/" + tool
	case strings.HasSuffix(u.Path, "/v1"):
		return base + "/tools/" + tool
	default:
		return base + "/v1/tools/" + tool
	}
}

// SanitizeQuery collapses whitespace and clips to DeepSeek's 1..128 Unicode rune limit.
func SanitizeQuery(q string) string {
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return ""
	}
	clean := strings.Join(fields, " ")
	if utf8.RuneCountInString(clean) <= deepseekMaxQueryRunes {
		return clean
	}
	return strings.TrimSpace(string([]rune(clean)[:deepseekMaxQueryRunes]))
}

// ValidateFetchURL validates that raw is an HTTP/HTTPS URL <= 8192 bytes with a public host
// and no embedded username/password credentials.
func ValidateFetchURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("网页地址不能为空")
	}
	if len(s) > deepseekMaxFetchURL {
		return "", errors.New("网页地址过长（超过 8192 字符）")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", errors.New("网页地址格式不正确")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("仅支持 http 或 https 网页链接")
	}
	if u.User != nil {
		return "", errors.New("网页链接不能包含用户名或密码")
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return "", errors.New("网页链接缺少域名")
	}
	if isBlockedHost(host) {
		return "", errors.New("不支持抓取本地或内网地址")
	}
	return u.String(), nil
}

func isBlockedHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func (d *deepSeekDriver) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	q := SanitizeQuery(req.Query)
	if q == "" {
		return nil, errors.New("搜索词不能为空")
	}
	count := req.Count
	if count <= 0 {
		count = 8
	} else if count > 50 {
		count = 50
	}

	payload := map[string]any{
		"query":        q,
		"count":        count,
		"need_content": req.NeedContent,
	}
	if ori := SanitizeQuery(req.OriginalQuestion); ori != "" {
		payload["ori_question"] = ori
	}
	if loc := strings.TrimSpace(req.Localization); loc != "" {
		payload["localization"] = loc
	}

	raw, err := d.postWithRetry(ctx, DeepSeekEndpoint(d.baseURL, "search"), payload)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Results []struct {
			RerankScore float64 `json:"rerank_score"`
			Title       string  `json:"title"`
			URL         string  `json:"url"`
			Snippet     string  `json:"snippet"`
			Summary     string  `json:"summary"`
			Content     string  `json:"content"`
			SiteName    *string `json:"site_name"`
			SiteIcon    *string `json:"site_icon"`
			Date        *string `json:"date"`
			Authority   int     `json:"authority"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, errors.New("解析搜索服务响应失败")
	}

	out := &SearchResponse{Results: make([]SearchResult, 0, len(parsed.Results))}
	for _, r := range parsed.Results {
		var siteName, siteIcon, date string
		if r.SiteName != nil {
			siteName = strings.TrimSpace(*r.SiteName)
		}
		if r.SiteIcon != nil {
			siteIcon = strings.TrimSpace(*r.SiteIcon)
		}
		if r.Date != nil {
			date = strings.TrimSpace(*r.Date)
		}
		out.Results = append(out.Results, SearchResult{
			Title:       strings.TrimSpace(r.Title),
			URL:         strings.TrimSpace(r.URL),
			Snippet:     strings.TrimSpace(r.Snippet),
			Summary:     strings.TrimSpace(r.Summary),
			Content:     strings.TrimSpace(r.Content),
			SiteName:    siteName,
			SiteIcon:    siteIcon,
			Date:        date,
			RerankScore: r.RerankScore,
			Authority:   r.Authority,
		})
	}
	if len(out.Results) > 0 {
		out.CostUSD = deepseekCallCostUSD
	}
	return out, nil
}

func (d *deepSeekDriver) Fetch(ctx context.Context, targetURL string) (*FetchResponse, error) {
	cleanURL, err := ValidateFetchURL(targetURL)
	if err != nil {
		return nil, err
	}
	raw, err := d.postWithRetry(ctx, DeepSeekEndpoint(d.baseURL, "fetch"), map[string]any{"url": cleanURL})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, errors.New("解析网页内容响应失败")
	}
	res := &FetchResponse{
		Title:   strings.TrimSpace(parsed.Title),
		Content: strings.TrimSpace(parsed.Content),
	}
	if res.Content != "" {
		res.CostUSD = deepseekCallCostUSD
	}
	return res, nil
}

func (d *deepSeekDriver) Test(ctx context.Context) (int, error) {
	res, err := d.Search(ctx, SearchRequest{Query: "今日要闻", Count: 3})
	if err != nil {
		return 0, err
	}
	return len(res.Results), nil
}

func (d *deepSeekDriver) postWithRetry(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	client := d.svc.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	for attempt := 0; ; attempt++ {
		raw, err := d.doPost(ctx, client, endpoint, body)
		if err == nil || attempt >= 1 || ctx.Err() != nil || !isRetryableSearchErr(err) {
			return raw, err
		}
		select {
		case <-time.After(650 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func isRetryableSearchErr(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Retryable()
	}
	// Transient network/transport error before response.
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (d *deepSeekDriver) doPost(ctx context.Context, client *http.Client, endpoint string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Cap read size at 4 MiB so an unusually large fetched Markdown page never exhausts memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		code, msg := parseDeepSeekError(raw)
		return nil, &APIError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	return raw, nil
}

func parseDeepSeekError(raw []byte) (string, string) {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil {
		code := e.Error.Code
		if code == "" {
			code = e.Error.Type
		}
		if e.Error.Message != "" {
			return code, e.Error.Message
		}
		if e.Message != "" {
			return code, e.Message
		}
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 240 {
		s = s[:240]
	}
	return "", s
}
