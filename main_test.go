package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMainFunction(t *testing.T) {
	oldArgs := os.Args
	oldExit := exit
	defer func() {
		os.Args = oldArgs
		exit = oldExit
	}()

	os.Args = []string{"infoblox-exporter", "-version"}
	exitCode := -1
	exit = func(code int) {
		exitCode = code
		panic("exit")
	}

	func() {
		defer func() {
			if recovered := recover(); recovered != "exit" {
				t.Fatalf("unexpected panic: %#v", recovered)
			}
		}()
		main()
	}()

	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", exitCode)
	}
}

func TestRunVersionAndParseError(t *testing.T) {
	clearInfobloxEnv(t)
	var stdout bytes.Buffer
	if code := run([]string{"-version"}, &stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("unexpected version code: %d", code)
	}
	if !strings.Contains(stdout.String(), "Infoblox-Exporter vdev build none") {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}

	if code := run([]string{"-not-a-real-flag"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("unexpected parse error code: %d", code)
	}
}

func TestRunValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
	}{
		{name: "missing url", env: map[string]string{}},
		{
			name: "missing credentials",
			args: []string{"-url", "http://gm.example.test/wapi/v2.13.7"},
		},
		{
			name: "invalid page size",
			args: []string{"-url", "http://gm.example.test/wapi/v2.13.7"},
			env: map[string]string{
				"INFOBLOX_USERNAME":  "user",
				"INFOBLOX_PASSWORD":  "pass",
				"INFOBLOX_PAGE_SIZE": "invalid",
			},
		},
		{
			name: "invalid timeout",
			args: []string{"-url", "http://gm.example.test/wapi/v2.13.7"},
			env: map[string]string{
				"INFOBLOX_USERNAME": "user",
				"INFOBLOX_PASSWORD": "pass",
				"INFOBLOX_TIMEOUT":  "invalid",
			},
		},
		{
			name: "invalid client",
			args: []string{"-url", "://bad"},
			env: map[string]string{
				"INFOBLOX_USERNAME": "user",
				"INFOBLOX_PASSWORD": "pass",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearInfobloxEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			if code := run(tt.args, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
				t.Fatalf("unexpected code: %d", code)
			}
		})
	}
}

func TestRunServerClosed(t *testing.T) {
	clearInfobloxEnv(t)
	t.Setenv("INFOBLOX_USERNAME", "user")
	t.Setenv("INFOBLOX_PASSWORD", "pass")
	t.Setenv("INFOBLOX_LABELS", "env=test")
	t.Setenv("INFOBLOX_DISABLED_MODULES", "dtc")
	t.Setenv("INFOBLOX_NETWORK_VIEWS", "default")
	t.Setenv("INFOBLOX_DNS_VIEWS", "default")
	t.Setenv("INFOBLOX_NETWORKS", "10.1.216.0/24")
	t.Setenv("INFOBLOX_ZONES", "example.test")
	t.Setenv("INFOBLOX_UPGRADE_STATUS_TYPES", "GRID")

	caPath := writeTestCert(t)
	restore := replaceRunHooks(t)
	defer restore()
	defaultRegisterer = prometheus.NewRegistry()
	listenAndServe = func(*http.Server) error {
		return http.ErrServerClosed
	}

	args := []string{
		"-url", "http://gm.example.test/wapi/v2.13.7",
		"-labels", "env=cli,site=lab",
		"-disabled-modules", "allrecords",
		"-bind-port", "9999",
		"-page-size", "10",
		"-timeout", "1s",
		"-ignore-cert",
		"-ca-file", caPath,
		"-network-views", "default",
		"-dns-views", "default",
		"-networks", "10.1.216.0/24",
		"-zones", "example.test",
		"-upgrade-status-types", "GRID",
		"-debug",
	}
	if code := run(args, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("unexpected code: %d", code)
	}
}

func TestRunSignalShutdown(t *testing.T) {
	clearInfobloxEnv(t)
	t.Setenv("INFOBLOX_URL", "http://gm.example.test/wapi/v2.13.7")
	t.Setenv("INFOBLOX_USERNAME", "user")
	t.Setenv("INFOBLOX_PASSWORD", "pass")
	t.Setenv("INFOBLOX_IGNORE_CERT", "true")
	t.Setenv("INFOBLOX_CA_FILE", writeTestCert(t))

	restore := replaceRunHooks(t)
	defer restore()
	unblock := make(chan struct{})
	done := make(chan struct{})
	listenAndServe = func(*http.Server) error {
		defer close(done)
		<-unblock
		return http.ErrServerClosed
	}
	shutdownServer = func(*http.Server, context.Context) error {
		close(unblock)
		<-done
		return nil
	}
	signalNotify = func(ch chan<- os.Signal, _ ...os.Signal) {
		ch <- syscall.SIGTERM
	}

	if code := run(nil, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("unexpected code: %d", code)
	}
}

func TestRunServerAndShutdownErrors(t *testing.T) {
	tests := []struct {
		name        string
		listenError error
		shutdownErr error
	}{
		{name: "server error", listenError: errors.New("listen failed")},
		{name: "shutdown error", listenError: http.ErrServerClosed, shutdownErr: errors.New("shutdown failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearInfobloxEnv(t)
			t.Setenv("INFOBLOX_URL", "http://gm.example.test/wapi/v2.13.7")
			t.Setenv("INFOBLOX_USERNAME", "user")
			t.Setenv("INFOBLOX_PASSWORD", "pass")
			restore := replaceRunHooks(t)
			defer restore()
			listenAndServe = func(*http.Server) error {
				return tt.listenError
			}
			shutdownServer = func(*http.Server, context.Context) error {
				return tt.shutdownErr
			}

			if code := run(nil, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
				t.Fatalf("unexpected code: %d", code)
			}
		})
	}
}

func TestNewMux(t *testing.T) {
	mux := newMux(prometheus.NewRegistry())

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/health", want: "OK"},
		{path: "/", want: "Infoblox-Exporter - /metrics for Prometheus metrics"},
		{path: "/metrics", want: ""},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d", tc.path, rec.Code)
		}
		if tc.want != "" && rec.Body.String() != tc.want {
			t.Fatalf("%s returned %q", tc.path, rec.Body.String())
		}
	}
}

func TestChooseCSV(t *testing.T) {
	defaultValue := []string{"default"}

	if got := chooseCSV("", "", defaultValue); !reflect.DeepEqual(got, defaultValue) {
		t.Fatalf("unexpected default value: %#v", got)
	}
	if got := chooseCSV("", "env-a,env-b", defaultValue); !reflect.DeepEqual(got, []string{"env-a", "env-b"}) {
		t.Fatalf("unexpected env value: %#v", got)
	}
	if got := chooseCSV("cli-a", "env-a", defaultValue); !reflect.DeepEqual(got, []string{"cli-a"}) {
		t.Fatalf("unexpected cli value: %#v", got)
	}
}

func TestChooseInt(t *testing.T) {
	got, err := chooseInt(0, "500", 1000, "page-size")
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Fatalf("unexpected env value: %d", got)
	}

	got, err = chooseInt(250, "500", 1000, "page-size")
	if err != nil {
		t.Fatal(err)
	}
	if got != 250 {
		t.Fatalf("unexpected cli value: %d", got)
	}

	if _, err := chooseInt(0, "invalid", 1000, "page-size"); err == nil {
		t.Fatalf("expected invalid env value to fail")
	}
	if _, err := chooseInt(0, "", 0, "page-size"); err == nil {
		t.Fatalf("expected non-positive value to fail")
	}
}

func TestChooseDuration(t *testing.T) {
	got, err := chooseDuration(0, "5s", 30*time.Second, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	if got != 5*time.Second {
		t.Fatalf("unexpected env value: %s", got)
	}

	got, err = chooseDuration(2*time.Second, "5s", 30*time.Second, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2*time.Second {
		t.Fatalf("unexpected cli value: %s", got)
	}

	if _, err := chooseDuration(0, "invalid", 30*time.Second, "timeout"); err == nil {
		t.Fatalf("expected invalid env value to fail")
	}
	if _, err := chooseDuration(0, "", 0, "timeout"); err == nil {
		t.Fatalf("expected non-positive value to fail")
	}
}

func clearInfobloxEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"INFOBLOX_URL",
		"INFOBLOX_WAPI_URL",
		"INFOBLOX_BASE_URL",
		"INFOBLOX_USERNAME",
		"INFOBLOX_PASSWORD",
		"INFOBLOX_IGNORE_CERT",
		"INFOBLOX_EXPORTER_INSECURE_SKIP_VERIFY",
		"INFOBLOX_CA_FILE",
		"INFOBLOX_LABELS",
		"INFOBLOX_DISABLED_MODULES",
		"INFOBLOX_PAGE_SIZE",
		"INFOBLOX_EXPORTER_PAGE_SIZE",
		"INFOBLOX_TIMEOUT",
		"INFOBLOX_EXPORTER_TIMEOUT",
		"INFOBLOX_NETWORK_VIEWS",
		"INFOBLOX_DNS_VIEWS",
		"INFOBLOX_NETWORKS",
		"INFOBLOX_ZONES",
		"INFOBLOX_UPGRADE_STATUS_TYPES",
	} {
		t.Setenv(key, "")
	}
}

func replaceRunHooks(t *testing.T) func() {
	t.Helper()
	oldListenAndServe := listenAndServe
	oldShutdownServer := shutdownServer
	oldSignalNotify := signalNotify
	oldSignalStop := signalStop
	oldDefaultRegisterer := defaultRegisterer

	shutdownServer = func(server *http.Server, ctx context.Context) error {
		return server.Shutdown(ctx)
	}
	signalNotify = func(chan<- os.Signal, ...os.Signal) {}
	signalStop = func(chan<- os.Signal) {}
	defaultRegisterer = nil

	return func() {
		listenAndServe = oldListenAndServe
		shutdownServer = oldShutdownServer
		signalNotify = oldSignalNotify
		signalStop = oldSignalStop
		defaultRegisterer = oldDefaultRegisterer
	}
}

func writeTestCert(t *testing.T) string {
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
	path := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
