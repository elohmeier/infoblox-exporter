package config

import (
	"os"
	"sort"
	"strings"
	"time"
)

type Config struct {
	Labels             map[string]string
	DisabledModules    []string
	Timeout            time.Duration
	PageSize           int
	NetworkViews       []string
	DNSViews           []string
	Networks           []string
	Zones              []string
	UpgradeStatusTypes []string
}

func Default() Config {
	return Config{
		Labels:   map[string]string{},
		Timeout:  30 * time.Second,
		PageSize: 1000,
		DNSViews: []string{"default"},
		UpgradeStatusTypes: []string{
			"GRID",
			"GROUP",
			"VNODE",
			"PNODE",
		},
	}
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

func GetNetworkViews() string {
	return os.Getenv("INFOBLOX_NETWORK_VIEWS")
}

func GetDNSViews() string {
	return os.Getenv("INFOBLOX_DNS_VIEWS")
}

func GetNetworks() string {
	return os.Getenv("INFOBLOX_NETWORKS")
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
