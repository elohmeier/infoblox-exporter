package config

import (
	"reflect"
	"testing"
)

func TestParseLabelsAndDisabledModules(t *testing.T) {
	labels := ParseLabels("env=prod, dc=de , ignored,service=ipam")
	if labels["env"] != "prod" || labels["dc"] != "de" || labels["service"] != "ipam" {
		t.Fatalf("unexpected labels: %#v", labels)
	}
	if _, ok := labels["ignored"]; ok {
		t.Fatalf("malformed label should be ignored: %#v", labels)
	}

	modules := ParseDisabledModules("range, ipv4address,,allrecords ")
	if !reflect.DeepEqual(modules, []string{"range", "ipv4address", "allrecords"}) {
		t.Fatalf("unexpected modules: %#v", modules)
	}
}

func TestEnvAccessors(t *testing.T) {
	t.Setenv("INFOBLOX_URL", "https://gm.example.test/wapi/v2.13.7")
	t.Setenv("INFOBLOX_USERNAME", "api-user")
	t.Setenv("INFOBLOX_PASSWORD", "api-pass")
	t.Setenv("INFOBLOX_IGNORE_CERT", "true")
	t.Setenv("INFOBLOX_CA_FILE", "/tmp/ca.pem")
	t.Setenv("INFOBLOX_LABELS", "env=test")
	t.Setenv("INFOBLOX_DISABLED_MODULES", "dtc")
	t.Setenv("INFOBLOX_PAGE_SIZE", "500")
	t.Setenv("INFOBLOX_TIMEOUT", "10s")
	t.Setenv("INFOBLOX_NETWORK_VIEWS", "default")
	t.Setenv("INFOBLOX_DNS_VIEWS", "default")
	t.Setenv("INFOBLOX_NETWORKS", "10.1.216.0/24")
	t.Setenv("INFOBLOX_ZONES", "example.test")
	t.Setenv("INFOBLOX_UPGRADE_STATUS_TYPES", "GRID")

	username, password := GetCredentials()
	if username != "api-user" || password != "api-pass" {
		t.Fatalf("unexpected auth: %s/%s", username, password)
	}
	if GetURL() != "https://gm.example.test/wapi/v2.13.7" {
		t.Fatalf("unexpected URL: %s", GetURL())
	}
	if !GetIgnoreCert() {
		t.Fatalf("ignore cert should be enabled")
	}
	if GetCAFile() != "/tmp/ca.pem" {
		t.Fatalf("unexpected CA file: %s", GetCAFile())
	}
	if GetNetworks() != "10.1.216.0/24" {
		t.Fatalf("unexpected networks: %s", GetNetworks())
	}
	if GetLabels() != "env=test" || GetDisabledModules() != "dtc" || GetPageSize() != "500" || GetTimeout() != "10s" {
		t.Fatalf("unexpected env values")
	}
	if GetNetworkViews() != "default" || GetDNSViews() != "default" || GetZones() != "example.test" || GetUpgradeStatusTypes() != "GRID" {
		t.Fatalf("unexpected scope env values")
	}
}

func TestEnvFallbacksAndDefaults(t *testing.T) {
	t.Setenv("INFOBLOX_WAPI_URL", "https://fallback.example.test/wapi/v2.13.7")
	t.Setenv("INFOBLOX_EXPORTER_INSECURE_SKIP_VERIFY", "1")
	t.Setenv("INFOBLOX_EXPORTER_PAGE_SIZE", "250")
	t.Setenv("INFOBLOX_EXPORTER_TIMEOUT", "5s")

	if GetURL() != "https://fallback.example.test/wapi/v2.13.7" {
		t.Fatalf("unexpected fallback URL: %s", GetURL())
	}
	if !GetIgnoreCert() {
		t.Fatalf("fallback ignore cert should be enabled")
	}
	if GetPageSize() != "250" || GetTimeout() != "5s" {
		t.Fatalf("unexpected fallback page/timeout values")
	}

	t.Setenv("INFOBLOX_WAPI_URL", "")
	t.Setenv("INFOBLOX_EXPORTER_INSECURE_SKIP_VERIFY", "")
	if GetURL() != "" {
		t.Fatalf("expected empty URL")
	}
	if GetIgnoreCert() {
		t.Fatalf("empty bool should be false")
	}
	if ParseCSV("") != nil {
		t.Fatalf("empty CSV should return nil")
	}
}

func TestConfigHelpers(t *testing.T) {
	defaults := Default()
	if defaults.Timeout == 0 || defaults.PageSize != 1000 || len(defaults.DNSViews) != 0 || len(defaults.UpgradeStatusTypes) != 4 {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}

	cfg := Config{
		Labels:          map[string]string{"z": "last", "a": "first"},
		DisabledModules: []string{"Range", "allrecords"},
	}
	if !cfg.IsModuleDisabled("range") {
		t.Fatalf("range should be disabled")
	}
	if cfg.IsModuleDisabled("network") {
		t.Fatalf("network should be enabled")
	}
	if !reflect.DeepEqual(cfg.LabelKeys(), []string{"a", "z"}) {
		t.Fatalf("unexpected label keys: %#v", cfg.LabelKeys())
	}
}
