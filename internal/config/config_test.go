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

	modules := ParseDisabledModules("range, ipv4inventory,,allrecords ")
	if !reflect.DeepEqual(modules, []string{"range", "ipv4inventory", "allrecords"}) {
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
	t.Setenv("INFOBLOX_REFRESH_INTERVAL", "5m")
	t.Setenv("INFOBLOX_REFRESH_TIMEOUT", "2m")
	t.Setenv("INFOBLOX_MAX_STALE", "15m")
	t.Setenv("INFOBLOX_NETWORK_VIEWS", "default")
	t.Setenv("INFOBLOX_DNS_VIEWS", "default")
	t.Setenv("INFOBLOX_NETWORKS", "192.0.2.0/24")
	t.Setenv("INFOBLOX_IPV4_INVENTORY_NETWORKS", "198.51.100.0/24")
	t.Setenv("INFOBLOX_IPV4_INVENTORY_SCAN_RANGES", "203.0.113.0/24")
	t.Setenv("INFOBLOX_IPV4_INVENTORY_NAME_REGEX", "server")
	t.Setenv("INFOBLOX_IPV4_INVENTORY_NETWORK_EA", "Inventory=servers")
	t.Setenv("INFOBLOX_IPV4_INVENTORY_PAGE_SIZE", "2000")
	t.Setenv("INFOBLOX_IPV4_INVENTORY_MAX_ADDRESSES", "100000")
	t.Setenv("INFOBLOX_IPV4_INVENTORY_TIMEOUT", "5m")
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
	if GetNetworks() != "192.0.2.0/24" {
		t.Fatalf("unexpected networks: %s", GetNetworks())
	}
	if GetIPv4InventoryNetworks() != "198.51.100.0/24" || GetIPv4InventoryScanRanges() != "203.0.113.0/24" {
		t.Fatalf("unexpected IPv4 inventory scope")
	}
	if GetIPv4InventoryNameRegex() != "server" || GetIPv4InventoryNetworkEA() != "Inventory=servers" {
		t.Fatalf("unexpected IPv4 inventory selectors")
	}
	if GetIPv4InventoryPageSize() != "2000" || GetIPv4InventoryMaxAddresses() != "100000" || GetIPv4InventoryTimeout() != "5m" {
		t.Fatalf("unexpected IPv4 inventory limits")
	}
	if GetLabels() != "env=test" || GetDisabledModules() != "dtc" || GetPageSize() != "500" || GetTimeout() != "10s" {
		t.Fatalf("unexpected env values")
	}
	if GetRefreshInterval() != "5m" || GetRefreshTimeout() != "2m" || GetMaxStale() != "15m" {
		t.Fatalf("unexpected cache env values")
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
	if defaults.Timeout == 0 || defaults.RefreshInterval == 0 || defaults.RefreshTimeout == 0 || defaults.MaxStale == 0 || defaults.PageSize != 1000 || len(defaults.DNSViews) != 0 || len(defaults.UpgradeStatusTypes) != 4 || defaults.IPv4InventoryPageSize != 2000 || defaults.IPv4InventoryMaxAddresses != 100000 || defaults.IPv4InventoryTimeout == 0 {
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

func TestValidateIPv4Inventory(t *testing.T) {
	valid := Default()
	valid.IPv4InventoryNetworks = []string{"192.0.2.1/24"}
	valid.IPv4InventoryScanRanges = []string{"192.0.2.0/24"}
	valid.IPv4InventoryNameRegex = "(?i)server"
	valid.IPv4InventoryNetworkEA = "Inventory=servers"
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if !valid.IPv4InventoryConfigured() {
		t.Fatalf("inventory should be configured")
	}

	tests := []Config{
		func() Config { c := Default(); c.IPv4InventoryNetworks = []string{"bad"}; return c }(),
		func() Config { c := Default(); c.IPv4InventoryScanRanges = []string{"2001:db8::/32"}; return c }(),
		func() Config { c := Default(); c.IPv4InventoryNameRegex = "["; return c }(),
		func() Config { c := Default(); c.IPv4InventoryNetworkEA = "missing-value="; return c }(),
		func() Config { c := Default(); c.IPv4InventoryPageSize = 0; return c }(),
		func() Config { c := Default(); c.IPv4InventoryMaxAddresses = 0; return c }(),
		func() Config { c := Default(); c.IPv4InventoryTimeout = 0; return c }(),
	}
	for i, cfg := range tests {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("case %d should fail validation", i)
		}
	}
}
