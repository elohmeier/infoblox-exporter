package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Config struct {
	Labels                    map[string]string
	DisabledModules           []string
	Timeout                   time.Duration
	RefreshInterval           time.Duration
	RefreshTimeout            time.Duration
	MaxStale                  time.Duration
	PageSize                  int
	NetworkViews              []string
	DNSViews                  []string
	Networks                  []string
	IPv4InventoryNetworks     []string
	IPv4InventoryScanRanges   []string
	IPv4InventoryNameRegex    string
	IPv4InventoryNetworkEA    string
	IPv4InventoryPageSize     int
	IPv4InventoryMaxAddresses int
	IPv4InventoryTimeout      time.Duration
	Zones                     []string
	UpgradeStatusTypes        []string
}

func Default() Config {
	return Config{
		Labels:                    map[string]string{},
		Timeout:                   30 * time.Second,
		RefreshInterval:           5 * time.Minute,
		RefreshTimeout:            2 * time.Minute,
		MaxStale:                  15 * time.Minute,
		PageSize:                  1000,
		IPv4InventoryPageSize:     2000,
		IPv4InventoryMaxAddresses: 100000,
		IPv4InventoryTimeout:      5 * time.Minute,
		UpgradeStatusTypes: []string{
			"GRID",
			"GROUP",
			"VNODE",
			"PNODE",
		},
	}
}

func (c Config) Validate() error {
	for name, prefixes := range map[string][]string{
		"ipv4-inventory-networks":    c.IPv4InventoryNetworks,
		"ipv4-inventory-scan-ranges": c.IPv4InventoryScanRanges,
	} {
		for _, value := range prefixes {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s contains invalid IPv4 CIDR %q", name, value)
			}
		}
	}
	if c.IPv4InventoryNameRegex != "" {
		if _, err := regexp.Compile(c.IPv4InventoryNameRegex); err != nil {
			return fmt.Errorf("ipv4-inventory-name-regex is invalid: %w", err)
		}
	}
	if c.IPv4InventoryNetworkEA != "" {
		parts := strings.SplitN(c.IPv4InventoryNetworkEA, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return errors.New("ipv4-inventory-network-ea must use non-empty name=value syntax")
		}
	}
	if c.IPv4InventoryPageSize <= 0 {
		return errors.New("ipv4-inventory-page-size must be greater than zero")
	}
	if c.IPv4InventoryMaxAddresses <= 0 {
		return errors.New("ipv4-inventory-max-addresses must be greater than zero")
	}
	if c.IPv4InventoryTimeout <= 0 {
		return errors.New("ipv4-inventory-timeout must be greater than zero")
	}
	return nil
}

func (c Config) IPv4InventoryConfigured() bool {
	return len(c.IPv4InventoryNetworks) > 0 ||
		len(c.IPv4InventoryScanRanges) > 0 ||
		c.IPv4InventoryNameRegex != "" ||
		c.IPv4InventoryNetworkEA != ""
}

func (c Config) IsModuleDisabled(name string) bool {
	for _, module := range c.DisabledModules {
		if strings.EqualFold(strings.TrimSpace(module), name) {
			return true
		}
	}
	return false
}

func (c Config) LabelKeys() []string {
	keys := make([]string, 0, len(c.Labels))
	for key := range c.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func GetCredentials() (username, password string) {
	return os.Getenv("INFOBLOX_USERNAME"), os.Getenv("INFOBLOX_PASSWORD")
}

func GetIgnoreCert() bool {
	return parseBool(firstEnv("INFOBLOX_IGNORE_CERT", "INFOBLOX_EXPORTER_INSECURE_SKIP_VERIFY"))
}

func GetCAFile() string {
	return os.Getenv("INFOBLOX_CA_FILE")
}

func GetURL() string {
	return firstEnv("INFOBLOX_URL", "INFOBLOX_WAPI_URL", "INFOBLOX_BASE_URL")
}

func GetLabels() string {
	return os.Getenv("INFOBLOX_LABELS")
}

func GetDisabledModules() string {
	return os.Getenv("INFOBLOX_DISABLED_MODULES")
}

func GetPageSize() string {
	return firstEnv("INFOBLOX_PAGE_SIZE", "INFOBLOX_EXPORTER_PAGE_SIZE")
}

func GetTimeout() string {
	return firstEnv("INFOBLOX_TIMEOUT", "INFOBLOX_EXPORTER_TIMEOUT")
}

func GetRefreshInterval() string {
	return os.Getenv("INFOBLOX_REFRESH_INTERVAL")
}

func GetRefreshTimeout() string {
	return os.Getenv("INFOBLOX_REFRESH_TIMEOUT")
}

func GetMaxStale() string {
	return os.Getenv("INFOBLOX_MAX_STALE")
}

func GetNetworkViews() string {
	return os.Getenv("INFOBLOX_NETWORK_VIEWS")
}

func GetDNSViews() string {
	return os.Getenv("INFOBLOX_DNS_VIEWS")
}

func GetNetworks() string {
	return os.Getenv("INFOBLOX_NETWORKS")
}

func GetIPv4InventoryNetworks() string {
	return os.Getenv("INFOBLOX_IPV4_INVENTORY_NETWORKS")
}

func GetIPv4InventoryScanRanges() string {
	return os.Getenv("INFOBLOX_IPV4_INVENTORY_SCAN_RANGES")
}

func GetIPv4InventoryNameRegex() string {
	return os.Getenv("INFOBLOX_IPV4_INVENTORY_NAME_REGEX")
}

func GetIPv4InventoryNetworkEA() string {
	return os.Getenv("INFOBLOX_IPV4_INVENTORY_NETWORK_EA")
}

func GetIPv4InventoryPageSize() string {
	return os.Getenv("INFOBLOX_IPV4_INVENTORY_PAGE_SIZE")
}

func GetIPv4InventoryMaxAddresses() string {
	return os.Getenv("INFOBLOX_IPV4_INVENTORY_MAX_ADDRESSES")
}

func GetIPv4InventoryTimeout() string {
	return os.Getenv("INFOBLOX_IPV4_INVENTORY_TIMEOUT")
}

func GetZones() string {
	return os.Getenv("INFOBLOX_ZONES")
}

func GetUpgradeStatusTypes() string {
	return os.Getenv("INFOBLOX_UPGRADE_STATUS_TYPES")
}

func ParseLabels(labelsStr string) map[string]string {
	labels := make(map[string]string)
	for _, pair := range ParseCSV(labelsStr) {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" {
			labels[key] = value
		}
	}
	return labels
}

func ParseDisabledModules(modulesStr string) []string {
	return ParseCSV(modulesStr)
}

func ParseCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}
