package collector

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"testing"

	"github.com/elohmeier/infoblox-exporter/internal/config"
	"github.com/elohmeier/infoblox-exporter/internal/wapi"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMergedInventoryIntervalsNormalizesAndMerges(t *testing.T) {
	intervals, err := mergedInventoryIntervals([]string{
		"192.0.2.17/25",
		"192.0.2.128/25",
		"192.0.2.128/26",
		"198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 2 {
		t.Fatalf("merged interval count = %d, want 2", len(intervals))
	}
	if ipv4Addr(intervals[0].start).String() != "192.0.2.0" || ipv4Addr(intervals[0].end).String() != "192.0.2.255" {
		t.Fatalf("unexpected first interval: %#v", intervals[0])
	}
	if ipv4Addr(intervals[1].start).String() != "198.51.100.0" || ipv4Addr(intervals[1].end).String() != "198.51.100.255" {
		t.Fatalf("unexpected second interval: %#v", intervals[1])
	}
	if _, err := mergedInventoryIntervals([]string{"2001:db8::/32"}); err == nil {
		t.Fatalf("IPv6 interval should be rejected")
	}
}

func TestInventoryBoundsStayInsideInterval(t *testing.T) {
	params := url.Values{}
	setInventoryBounds(params, inventoryInterval{
		start: ipv4Uint32(netip.MustParseAddr("192.0.2.0")),
		end:   ipv4Uint32(netip.MustParseAddr("192.0.2.255")),
	})
	if params.Get("ip_address>") != "192.0.2.0" || params.Get("ip_address<") != "192.0.2.255" {
		t.Fatalf("unexpected inventory bounds: %v", params)
	}
}

func TestInventoryQueryPlanning(t *testing.T) {
	selector := &inventoryNetworkSelector{
		anyView: map[string]struct{}{"192.0.2.0/25": {}},
		byView: map[string]map[string]struct{}{
			"blue": {"192.0.2.128/25": {}},
		},
	}
	cfg := config.Default()
	cfg.NetworkViews = []string{"blue", "green"}
	exporter := &Exporter{cfg: cfg}
	queries, err := exporter.inventoryQueries(selector)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Fatalf("adjacent blue networks and the green network should produce two queries, got %d", len(queries))
	}
	if queries[0].view != "blue" || ipv4Addr(queries[0].interval.start).String() != "192.0.2.0" || ipv4Addr(queries[0].interval.end).String() != "192.0.2.255" {
		t.Fatalf("unexpected blue query: %#v", queries[0])
	}
	if queries[1].view != "green" || ipv4Addr(queries[1].interval.end).String() != "192.0.2.127" {
		t.Fatalf("unexpected green query: %#v", queries[1])
	}

	cfg.IPv4InventoryScanRanges = []string{"192.0.2.0/25", "192.0.2.128/25"}
	exporter.cfg = cfg
	queries, err = exporter.inventoryQueries(selector)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || uniqueBoundedIntervals(queries) != 1 {
		t.Fatalf("one merged scan interval should be queried once per view: %#v", queries)
	}

	cfg.IPv4InventoryScanRanges = nil
	cfg.IPv4InventoryNameRegex = "server"
	exporter.cfg = cfg
	emptySelector := &inventoryNetworkSelector{anyView: map[string]struct{}{}, byView: map[string]map[string]struct{}{}}
	queries, err = exporter.inventoryQueries(emptySelector)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || queries[0].bounded || queries[1].bounded {
		t.Fatalf("name-only selection should create one unbounded query per view: %#v", queries)
	}
}

func TestIPv4InventorySelectsNetworksByEAAndName(t *testing.T) {
	var networkRequests int
	var addressRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wapi/v2.13.7/network":
			networkRequests++
			if r.URL.Query().Get("*Inventory") != "servers" || r.URL.Query().Get("network_view") != "blue" {
				t.Fatalf("unexpected network EA query: %s", r.URL.RawQuery)
			}
			writeResult(t, w, []map[string]interface{}{{"network": "198.51.100.17/24", "network_view": "blue"}})
		case "/wapi/v2.13.7/ipv4address":
			addressRequests++
			query := r.URL.Query()
			if query.Get("network_view") != "blue" || query.Get("status") != "USED" || query.Get("names~") != "(?i)server" {
				t.Fatalf("unexpected IPv4 inventory query: %s", r.URL.RawQuery)
			}
			if query.Get("ip_address>") != "198.51.100.0" || query.Get("ip_address<") != "198.51.100.255" {
				t.Fatalf("unexpected IPv4 inventory bounds: %s", r.URL.RawQuery)
			}
			writeResult(t, w, []map[string]interface{}{
				{"ip_address": "198.51.100.11", "network": "198.51.100.0/24", "network_view": "blue", "status": "USED", "names": []string{"server-2"}},
				{"ip_address": "198.51.100.10", "network": "198.51.100.0/24", "network_view": "blue", "status": "USED", "names": []string{"server-1"}},
				{"ip_address": "198.51.100.200", "network": "198.51.100.128/25", "network_view": "blue", "status": "USED", "names": []string{"nested-server"}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExcept("ipv4inventory")
	cfg.NetworkViews = []string{"blue"}
	cfg.IPv4InventoryNetworkEA = "Inventory=servers"
	cfg.IPv4InventoryNameRegex = "(?i)server"
	exporter, registry := newInventoryTestExporter(t, cfg, server.URL)
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if networkRequests != 1 || addressRequests != 1 {
		t.Fatalf("unexpected requests: networks=%d addresses=%d", networkRequests, addressRequests)
	}
	if familyMetricCount(families, "infoblox_ipv4inventory_info") != 2 {
		t.Fatalf("EA exact-network filter should emit two addresses")
	}
	if metricValue(t, families, "infoblox_ipv4inventory_selected_networks") != 1 || metricValue(t, families, "infoblox_ipv4inventory_scan_ranges") != 1 {
		t.Fatalf("unexpected selected network or scan range count")
	}
	if len(exporter.ipv4Inventory) != 2 || exporter.ipv4Inventory[0].IPAddress != "198.51.100.10" || exporter.ipv4Inventory[1].IPAddress != "198.51.100.11" {
		t.Fatalf("inventory cache is not deterministically sorted: %#v", exporter.ipv4Inventory)
	}
}

func TestIPv4InventoryCapFailurePreservesCache(t *testing.T) {
	overflow := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wapi/v2.13.7/ipv4address" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		result := []map[string]interface{}{
			{"ip_address": "203.0.113.10", "network": "203.0.113.0/24", "network_view": "default", "status": "USED"},
		}
		if overflow {
			result = append(result, map[string]interface{}{"ip_address": "203.0.113.11", "network": "203.0.113.0/24", "network_view": "default", "status": "USED"})
		}
		writeResult(t, w, result)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExcept("ipv4inventory")
	cfg.IPv4InventoryNetworks = []string{"203.0.113.0/24"}
	cfg.IPv4InventoryMaxAddresses = 1
	exporter, registry := newInventoryTestExporter(t, cfg, server.URL)
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	overflow = true
	if err := exporter.RefreshOnce(context.Background()); err == nil {
		t.Fatalf("address cap overflow should fail the refresh")
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if familyMetricCount(families, "infoblox_ipv4inventory_info") != 1 {
		t.Fatalf("failed refresh should preserve the previous inventory cache")
	}
	if metricValueForLabels(t, families, "infoblox_collector_up", map[string]string{"collector": "ipv4inventory"}) != 0 {
		t.Fatalf("collector health should expose the failed inventory attempt")
	}
	metricForLabels(t, families, "infoblox_ipv4inventory_info", map[string]string{
		"ip_address": "203.0.113.10", "network": "203.0.113.0/24", "network_view": "default", "status": "USED", "names": "", "types": "", "usage": "",
	})
}

func newInventoryTestExporter(t *testing.T, cfg config.Config, serverURL string) (*Exporter, *prometheus.Registry) {
	t.Helper()
	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  serverURL + "/wapi/v2.13.7",
		Username: "user",
		Password: "pass",
		PageSize: cfg.PageSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	exporter := New(cfg, client, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	registry := prometheus.NewRegistry()
	registry.MustRegister(exporter)
	return exporter, registry
}
