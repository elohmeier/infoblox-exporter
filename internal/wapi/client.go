package wapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	systemCertPool = x509.SystemCertPool
	readFile       = os.ReadFile
	newRequest     = http.NewRequestWithContext
)

type Config struct {
	BaseURL            string
	Username           string
	Password           string
	Timeout            time.Duration
	PageSize           int
	InsecureSkipVerify bool
	CAFile             string
	UserAgent          string
	Metrics            *Metrics
}

type Client struct {
	baseURL    *url.URL
	username   string
	password   string
	pageSize   int
	userAgent  string
	httpClient *http.Client
	metrics    *Metrics
}

type WAPIError struct {
	StatusCode int
	Code       string
	ErrorText  string
	Text       string
}

func (e WAPIError) Error() string {
	if e.Text != "" {
		return fmt.Sprintf("infoblox wapi status=%d code=%s text=%s", e.StatusCode, e.Code, e.Text)
	}
	return fmt.Sprintf("infoblox wapi status=%d error=%s", e.StatusCode, e.ErrorText)
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.PageSize <= 0 {
		cfg.PageSize = 1000
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "infoblox-exporter/dev"
	}

	baseURL, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("base URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, errors.New("base URL must include host")
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec
	if cfg.CAFile != "" {
		pool, err := systemCertPool()
		if err != nil {
			return nil, err
		}
		if pool == nil {
			pool = x509.NewCertPool()
		}
		caData, err := readFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to append CA certificates from %s", cfg.CAFile)
		}
		tlsConfig.RootCAs = pool
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}

	return &Client{
		baseURL:    baseURL,
		username:   cfg.Username,
		password:   cfg.Password,
		pageSize:   cfg.PageSize,
		userAgent:  cfg.UserAgent,
		httpClient: &http.Client{Timeout: cfg.Timeout, Transport: transport},
		metrics:    cfg.Metrics,
	}, nil
}

func FetchAll[T any](ctx context.Context, c *Client, object string, params url.Values) ([]T, error) {
	raw, err := c.FetchAllRaw(ctx, object, params)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(raw))
	for _, item := range raw {
		var decoded T
		if err := json.Unmarshal(item, &decoded); err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}

func (c *Client) FetchAllRaw(ctx context.Context, object string, params url.Values) ([]json.RawMessage, error) {
	query := cloneValues(params)
	query.Set("_paging", "1")
	query.Set("_return_as_object", "1")
	if query.Get("_max_results") == "" {
		query.Set("_max_results", strconv.Itoa(c.pageSize))
	}

	var all []json.RawMessage
	for {
		var raw json.RawMessage
		if err := c.get(ctx, object, query, &raw); err != nil {
			return nil, err
		}

		result, nextPageID, err := decodePage(raw)
		if err != nil {
			return nil, err
		}
		all = append(all, result...)
		if nextPageID == "" {
			break
		}
		query = url.Values{"_page_id": []string{nextPageID}}
	}
	return all, nil
}

func (c *Client) get(ctx context.Context, object string, params url.Values, dest interface{}) error {
	u := c.objectURL(object, params)
	req, err := newRequest(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start).Seconds()
	if err != nil {
		c.metrics.observe(object, "error", elapsed)
		return err
	}
	defer resp.Body.Close()
	c.metrics.observe(object, strconv.Itoa(resp.StatusCode), elapsed)

	if resp.StatusCode >= 400 {
		return decodeWAPIError(resp)
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	return decoder.Decode(dest)
}

func (c *Client) objectURL(object string, params url.Values) url.URL {
	u := *c.baseURL
	basePath := strings.TrimRight(u.Path, "/")
	objectPath := strings.TrimLeft(object, "/")
	u.Path = basePath + "/" + objectPath
	u.RawQuery = params.Encode()
	return u
}

type page struct {
	Result     []json.RawMessage `json:"result"`
	NextPageID string            `json:"next_page_id"`
}

func decodePage(raw json.RawMessage) ([]json.RawMessage, string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, "", errors.New("empty WAPI response")
	}
	if strings.HasPrefix(trimmed, "[") {
		var result []json.RawMessage
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, "", err
		}
		return result, "", nil
	}

	var p page
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", err
	}
	if p.Result == nil {
		return []json.RawMessage{raw}, "", nil
	}
	return p.Result, p.NextPageID, nil
}

func decodeWAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		Error string `json:"Error"`
		Code  string `json:"code"`
		Text  string `json:"text"`
	}
	_ = json.Unmarshal(body, &parsed)
	if parsed.Error == "" && parsed.Text == "" {
		parsed.Error = strings.TrimSpace(string(body))
	}
	return WAPIError{
		StatusCode: resp.StatusCode,
		Code:       parsed.Code,
		ErrorText:  parsed.Error,
		Text:       parsed.Text,
	}
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}
