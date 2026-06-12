package wapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/elohmeier/infoblox-exporter/internal/model"
	"github.com/prometheus/client_golang/prometheus"
)

func TestFetchAllUsesPagingAndBasicAuth(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/wapi/v2.13.7/network" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "user" || password != "pass" {
			t.Fatalf("missing or invalid basic auth")
		}

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("_page_id") == "" {
			if r.URL.Query().Get("_paging") != "1" {
				t.Fatalf("missing _paging on initial request")
			}
			if r.URL.Query().Get("_return_as_object") != "1" {
				t.Fatalf("missing _return_as_object on initial request")
			}
			if r.URL.Query().Get("_max_results") != "2" {
				t.Fatalf("unexpected _max_results: %s", r.URL.Query().Get("_max_results"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"next_page_id": "abc123",
				"result": []map[string]interface{}{
					{"network": "10.0.0.0/24", "network_view": "default"},
				},
			})
			return
		}

		if got := r.URL.Query().Get("_page_id"); got != "abc123" {
			t.Fatalf("unexpected _page_id: %s", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result": []map[string]interface{}{
				{"network": "10.0.1.0/24", "network_view": "default"},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
		Username: "user",
		Password: "pass",
		PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	networks, err := FetchAll[model.Network](context.Background(), client, "network", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(networks))
	}
	if networks[0].Network != "10.0.0.0/24" || networks[1].Network != "10.0.1.0/24" {
		t.Fatalf("unexpected networks: %#v", networks)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
}

func TestFetchAllDecodesWAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Error":"AdmConProtoError: Unknown argument","code":"Client.Ibap.Proto","text":"Unknown argument"}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = FetchAll[model.Network](context.Background(), client, "network", url.Values{})
	var wapiErr WAPIError
	if !errors.As(err, &wapiErr) {
		t.Fatalf("expected WAPIError, got %T: %v", err, err)
	}
	if wapiErr.StatusCode != http.StatusBadRequest || wapiErr.Code != "Client.Ibap.Proto" {
		t.Fatalf("unexpected WAPI error: %#v", wapiErr)
	}
}

func TestWAPIErrorError(t *testing.T) {
	if got := (WAPIError{StatusCode: 400, Code: "Client", Text: "bad request"}).Error(); !strings.Contains(got, "text=bad request") {
		t.Fatalf("unexpected text error: %s", got)
	}
	if got := (WAPIError{StatusCode: 500, ErrorText: "plain error"}).Error(); !strings.Contains(got, "error=plain error") {
		t.Fatalf("unexpected plain error: %s", got)
	}
}

func TestNewClientValidationAndCAHandling(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "%"}); err == nil {
		t.Fatalf("expected invalid URL to fail")
	}
	if _, err := NewClient(Config{BaseURL: "ftp://gm.example.test"}); err == nil {
		t.Fatalf("expected invalid scheme to fail")
	}
	if _, err := NewClient(Config{BaseURL: "http:///wapi/v2.13.7"}); err == nil {
		t.Fatalf("expected missing host to fail")
	}

	client, err := NewClient(Config{BaseURL: "http://gm.example.test/wapi/v2.13.7/"})
	if err != nil {
		t.Fatal(err)
	}
	if client.pageSize != 1000 || client.userAgent != "infoblox-exporter/dev" {
		t.Fatalf("defaults not applied: %#v", client)
	}

	oldSystemCertPool := systemCertPool
	oldReadFile := readFile
	defer func() {
		systemCertPool = oldSystemCertPool
		readFile = oldReadFile
	}()

	systemCertPool = func() (*x509.CertPool, error) {
		return nil, errors.New("system pool failed")
	}
	if _, err := NewClient(Config{BaseURL: "http://gm.example.test", CAFile: "ca.pem"}); err == nil {
		t.Fatalf("expected system pool error")
	}

	systemCertPool = func() (*x509.CertPool, error) {
		return x509.NewCertPool(), nil
	}
	readFile = func(string) ([]byte, error) {
		return nil, errors.New("read failed")
	}
	if _, err := NewClient(Config{BaseURL: "http://gm.example.test", CAFile: "ca.pem"}); err == nil {
		t.Fatalf("expected read error")
	}

	readFile = func(string) ([]byte, error) {
		return []byte("not a cert"), nil
	}
	if _, err := NewClient(Config{BaseURL: "http://gm.example.test", CAFile: "ca.pem"}); err == nil {
		t.Fatalf("expected invalid CA error")
	}

	systemCertPool = func() (*x509.CertPool, error) {
		return nil, nil
	}
	readFile = func(string) ([]byte, error) {
		return testCertificatePEM(t), nil
	}
	if _, err := NewClient(Config{BaseURL: "http://gm.example.test", CAFile: "ca.pem"}); err != nil {
		t.Fatal(err)
	}
}

func TestFetchAllDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"result": []map[string]interface{}{{"n": "bad"}},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL + "/wapi/v2.13.7"})
	if err != nil {
		t.Fatal(err)
	}
	var out []struct {
		N int `json:"n"`
	}
	out, err = FetchAll[struct {
		N int `json:"n"`
	}](context.Background(), client, "network", url.Values{})
	if err == nil {
		t.Fatalf("expected decode error, got %#v", out)
	}
}

func TestFetchAllRawPreservesMaxResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("_max_results"); got != "7" {
			t.Fatalf("unexpected _max_results: %s", got)
		}
		writeJSON(t, w, []map[string]interface{}{{"network": "10.0.0.0/24"}})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL + "/wapi/v2.13.7"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := client.FetchAllRaw(context.Background(), "network", url.Values{"_max_results": []string{"7"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("unexpected raw count: %d", len(raw))
	}
}

func TestFetchAllRawDecodePageError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`"not a page"`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL + "/wapi/v2.13.7"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchAllRaw(context.Background(), "network", url.Values{}); err == nil {
		t.Fatalf("expected decode page error")
	}
}

func TestGetRequestAndDecodeErrors(t *testing.T) {
	client := &Client{
		baseURL:    mustParseURL(t, "http://gm.example.test/wapi/v2.13.7"),
		httpClient: &http.Client{},
	}

	oldNewRequest := newRequest
	newRequest = func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, errors.New("request failed")
	}
	if err := client.get(context.Background(), "network", url.Values{}, &json.RawMessage{}); err == nil {
		t.Fatalf("expected request error")
	}
	newRequest = oldNewRequest

	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed")
	})}
	if err := client.get(context.Background(), "network", url.Values{}, &json.RawMessage{}); err == nil {
		t.Fatalf("expected transport error")
	}

	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("not-json")),
			Header:     http.Header{},
		}, nil
	})}
	if err := client.get(context.Background(), "network", url.Values{}, &json.RawMessage{}); err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestDecodePageVariants(t *testing.T) {
	if _, _, err := decodePage(nil); err == nil {
		t.Fatalf("expected empty page error")
	}
	result, next, err := decodePage(json.RawMessage(`[{"network":"10.0.0.0/24"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || next != "" {
		t.Fatalf("unexpected array page: result=%d next=%q", len(result), next)
	}
	if _, _, err := decodePage(json.RawMessage(`[`)); err == nil {
		t.Fatalf("expected bad array error")
	}
	if _, _, err := decodePage(json.RawMessage(`"not an object"`)); err == nil {
		t.Fatalf("expected bad object error")
	}
	result, next, err = decodePage(json.RawMessage(`{"network":"10.0.0.0/24"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || next != "" {
		t.Fatalf("unexpected singleton page: result=%d next=%q", len(result), next)
	}
}

func TestDecodePlainWAPIError(t *testing.T) {
	err := decodeWAPIError(&http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("plain failure")),
	})
	var wapiErr WAPIError
	if !errors.As(err, &wapiErr) {
		t.Fatalf("expected WAPIError: %T", err)
	}
	if wapiErr.ErrorText != "plain failure" {
		t.Fatalf("unexpected error text: %#v", wapiErr)
	}
}

func TestCloneValues(t *testing.T) {
	in := url.Values{"a": []string{"b"}}
	out := cloneValues(in)
	out.Set("a", "changed")
	if in.Get("a") != "b" {
		t.Fatalf("clone modified original: %#v", in)
	}
}

func TestMetrics(t *testing.T) {
	var nilMetrics *Metrics
	if nilMetrics.Collectors() != nil {
		t.Fatalf("nil metrics should have no collectors")
	}
	nilMetrics.observe("network", "200", 1)

	metrics := NewMetrics("test")
	if len(metrics.Collectors()) != 2 {
		t.Fatalf("unexpected collector count")
	}
	metrics.observe("network", "200", 0.01)

	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.Collectors()...)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) == 0 {
		t.Fatalf("expected metric families")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func writeJSON(t *testing.T, w http.ResponseWriter, value interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func testCertificatePEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestObjectURLTrimsSlashes(t *testing.T) {
	client := &Client{baseURL: mustParseURL(t, "http://gm.example.test/wapi/v2.13.7/")}
	u := client.objectURL("/network", url.Values{"a": []string{"b"}})
	if got := u.String(); got != "http://gm.example.test/wapi/v2.13.7/network?a=b" {
		t.Fatalf("unexpected URL: %s", got)
	}
}

func TestDecodeWAPIErrorIgnoresOversizedBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 1<<20+10)
	err := decodeWAPIError(&http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(bytes.NewReader(body)),
	})
	var wapiErr WAPIError
	if !errors.As(err, &wapiErr) {
		t.Fatalf("expected WAPIError: %T", err)
	}
	if len(wapiErr.ErrorText) != 1<<20 {
		t.Fatalf("unexpected limited body length: %d", len(wapiErr.ErrorText))
	}
}
