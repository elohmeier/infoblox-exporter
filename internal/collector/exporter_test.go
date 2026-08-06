package collector

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/elohmeier/infoblox-exporter/internal/config"
	"github.com/elohmeier/infoblox-exporter/internal/wapi"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestExporterCollectsCoreMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/wapi/v2.13.7/network":
			writeResult(t, w, []map[string]interface{}{
				{
					"_ref":                    "network/ref:192.0.2.0/24/default",
					"network":                 "192.0.2.0/24",
					"network_view":            "default",
					"utilization":             500,
					"dhcp_utilization":        250,
					"dhcp_utilization_status": "NORMAL",
					"utilization_update":      1_700_000_000,
				},
			})
		case "/wapi/v2.13.7/range":
			writeResult(t, w, []map[string]interface{}{
				{
					"_ref":                    "range/ref:192.0.2.10/192.0.2.100/default",
					"network":                 "192.0.2.0/24",
					"network_view":            "default",
					"start_addr":              "192.0.2.10",
					"end_addr":                "192.0.2.100",
					"dhcp_utilization":        1000,
					"dhcp_utilization_status": "LOW",
					"dynamic_hosts":           2,
					"server_association_type": "MEMBER",
				},
			})
		case "/wapi/v2.13.7/ipv4address":
			writeResult(t, w, []map[string]interface{}{
				{
					"ip_address":   "192.0.2.1",
					"network":      "192.0.2.0/24",
					"network_view": "default",
					"status":       "USED",
					"lease_state":  "ACTIVE",
					"types":        []string{"HOST"},
					"usage":        []string{"DNS", "DHCP"},
				},
				{
					"ip_address":   "192.0.2.254",
					"network":      "192.0.2.0/24",
					"network_view": "default",
					"status":       "USED",
					"types":        []string{"UNMANAGED"},
					"is_conflict":  true,
				},
			})
		case "/wapi/v2.13.7/member":
			writeResult(t, w, []map[string]interface{}{
				{
					"host_name":                  "gm.example.test",
					"platform":                   "VNIOS",
					"service_type_configuration": "ALL_V4",
					"service_status": []map[string]interface{}{
						{"service": "DNS", "status": "WORKING"},
					},
				},
			})
		case "/wapi/v2.13.7/restartservicestatus":
			writeResult(t, w, []map[string]interface{}{
				{
					"member":           "gm.example.test",
					"dhcp_status":      "NO_REQUEST",
					"dns_status":       "REQUESTING",
					"reporting_status": "DISABLED",
				},
			})
		case "/wapi/v2.13.7/grid:servicerestart:status":
			writeResult(t, w, []map[string]interface{}{
				{
					"parent":          "grid/ref:Infoblox",
					"grouped":         "GRID",
					"needed_restart":  1,
					"pending_restart": 1,
					"success":         2,
					"timeouts":        0,
				},
			})
		case "/wapi/v2.13.7/grid:servicerestart:request":
			writeResult(t, w, []map[string]interface{}{
				{
					"member":            "gm.example.test",
					"group":             "default",
					"service":           "DNS",
					"state":             "QUEUED",
					"needed":            "YES",
					"result":            "NORESTART",
					"forced":            false,
					"last_updated_time": 1_700_000_001,
				},
			})
		case "/wapi/v2.13.7/capacityreport":
			writeResult(t, w, []map[string]interface{}{
				{
					"name":          "gm.example.test",
					"hardware_type": "VNIOS",
					"role":          "GRID_MASTER",
					"max_capacity":  1_000_000,
					"percent_used":  25,
					"total_objects": 250_000,
					"object_counts": []map[string]interface{}{
						{"type": "network", "count": 10},
					},
				},
			})
		case "/wapi/v2.13.7/member:license":
			writeResult(t, w, []map[string]interface{}{
				{
					"type":              "DNS",
					"kind":              "Static",
					"limit":             "none",
					"limit_context":     "NONE",
					"expiration_status": "PERMANENT",
					"expiry_date":       0,
					"hwid":              "hwid-1",
				},
			})
		case "/wapi/v2.13.7/license:gridwide":
			writeResult(t, w, []map[string]interface{}{
				{
					"type":              "DHCP",
					"limit":             "1000",
					"limit_context":     "LEASES",
					"expiration_status": "NOT_EXPIRED",
					"expiry_date":       1_800_000_000,
				},
			})
		case "/wapi/v2.13.7/upgradestatus":
			writeResult(t, w, []map[string]interface{}{
				{
					"type":                     r.URL.Query().Get("type"),
					"member":                   "gm.example.test",
					"current_version":          "9.0.6",
					"distribution_version":     "9.0.7",
					"upload_version":           "9.0.7",
					"element_status":           "WORKING",
					"grid_state":               "NORMAL",
					"ha_status":                "ACTIVE",
					"status_value":             "COMPLETED",
					"status_value_update_time": 1_700_000_002,
					"steps_completed":          3,
					"steps_total":              3,
					"upgrade_state":            "NONE",
					"upgrade_test_status":      "NO_STATUS",
				},
			})
		case "/wapi/v2.13.7/dhcp:statistics":
			writeObject(t, w, map[string]interface{}{
				"dhcp_utilization":        120,
				"dhcp_utilization_status": "NORMAL",
				"dynamic_hosts":           4,
				"static_hosts":            2,
				"total_hosts":             6,
			})
		case "/wapi/v2.13.7/ipam:statistics":
			writeObject(t, w, map[string]interface{}{
				"network":            "192.0.2.0/24",
				"network_view":       "default",
				"cidr":               24,
				"conflict_count":     1,
				"unmanaged_count":    1,
				"utilization":        500,
				"utilization_update": 1_700_000_003,
			})
		case "/wapi/v2.13.7/dhcpfailover":
			writeResult(t, w, []map[string]interface{}{
				{
					"name":                  "fo-1",
					"association_type":      "GRID",
					"comment":               "test failover",
					"primary":               "member-a",
					"secondary":             "member-b",
					"primary_server_type":   "MEMBER",
					"secondary_server_type": "MEMBER",
					"load_balance_split":    128,
					"max_client_lead_time":  3600,
				},
			})
		case "/wapi/v2.13.7/allrecords":
			writeResult(t, w, []map[string]interface{}{
				{
					"name":        "host.example.test",
					"type":        "record:a",
					"view":        "default",
					"zone":        "example.test",
					"disable":     false,
					"reclaimable": true,
					"ttl":         3600,
				},
				{
					"name":    "old.example.test",
					"type":    "record:cname",
					"view":    "default",
					"zone":    "example.test",
					"disable": true,
				},
			})
		case "/wapi/v2.13.7/zone_auth":
			writeResult(t, w, []map[string]interface{}{
				{
					"fqdn":    "example.test",
					"view":    "default",
					"comment": "authoritative",
				},
			})
		case "/wapi/v2.13.7/zone_forward", "/wapi/v2.13.7/zone_stub", "/wapi/v2.13.7/zone_delegated", "/wapi/v2.13.7/zone_rp":
			writeResult(t, w, []map[string]interface{}{})
		case "/wapi/v2.13.7/dtc:object":
			writeResult(t, w, []map[string]interface{}{
				{
					"name":          "dtc-1",
					"comment":       "test dtc",
					"abstract_type": "dtc:lbdn",
					"display_type":  "LBDN",
					"status":        "OK",
				},
			})
		case "/wapi/v2.13.7/threatprotection:statistics":
			writeResult(t, w, []map[string]interface{}{
				{
					"member": "gm.example.test",
					"stat_infos": []map[string]interface{}{
						{"events": 7, "nested": map[string]interface{}{"drops": 3}},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.NetworkViews = []string{"default"}
	cfg.DNSViews = []string{"default"}
	cfg.Networks = []string{"192.0.2.0/24"}
	cfg.IPv4InventoryNetworks = []string{"192.0.2.0/24"}
	cfg.Zones = []string{"example.test"}

	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
		Username: "user",
		Password: "pass",
		PageSize: cfg.PageSize,
	})
	if err != nil {
		t.Fatal(err)
	}

	registry := prometheus.NewRegistry()
	exporter := New(cfg, client, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	registry.MustRegister(exporter)
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	names := map[string]bool{}
	for _, family := range families {
		names[family.GetName()] = true
		if strings.HasPrefix(family.GetName(), "infoblox_ipv4address_") {
			t.Fatalf("removed IPv4 address metric family is still exposed: %s", family.GetName())
		}
	}
	for _, expected := range []string{
		"infoblox_up",
		"infoblox_network_info",
		"infoblox_network_utilization_ratio",
		"infoblox_range_info",
		"infoblox_ipv4inventory_info",
		"infoblox_ipv4inventory_address_count",
		"infoblox_member_info",
		"infoblox_member_service_status",
		"infoblox_restart_service_status",
		"infoblox_service_restart_status_count",
		"infoblox_service_restart_request_info",
		"infoblox_capacity_info",
		"infoblox_capacity_used_ratio",
		"infoblox_license_info",
		"infoblox_upgrade_status_info",
		"infoblox_dhcp_statistics_utilization_ratio",
		"infoblox_ipam_statistics_utilization_ratio",
		"infoblox_dhcp_failover_info",
		"infoblox_dns_record_info",
		"infoblox_dns_record_ttl_seconds",
		"infoblox_dns_record_count",
		"infoblox_dns_zone_info",
		"infoblox_dtc_object_info",
		"infoblox_threatprotection_stat_value",
	} {
		if !names[expected] {
			t.Fatalf("missing metric family %s", expected)
		}
	}
}

func TestExporterUsesIndependentNetworkAndInventoryScopes(t *testing.T) {
	tests := []struct {
		name               string
		networks           []string
		wantInventoryScope string
	}{
		{
			name:               "operational collectors remain unfiltered",
			wantInventoryScope: "",
		},
		{
			name:               "operational and inventory scopes differ",
			networks:           []string{"198.51.100.0/24"},
			wantInventoryScope: "198.51.100.0/24",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := map[string]url.Values{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries[r.URL.Path] = r.URL.Query()
				writeResult(t, w, []map[string]interface{}{})
			}))
			defer server.Close()

			cfg := config.Default()
			cfg.DisabledModules = allModulesExceptAny("network", "range", "ipv4inventory")
			cfg.Networks = tt.networks
			cfg.IPv4InventoryNetworks = []string{"192.0.2.0/24"}
			client, err := wapi.NewClient(wapi.Config{
				BaseURL:  server.URL + "/wapi/v2.13.7",
				Username: "user",
				Password: "pass",
				PageSize: cfg.PageSize,
			})
			if err != nil {
				t.Fatal(err)
			}

			registry := prometheus.NewRegistry()
			exporter := New(cfg, client, slog.New(slog.NewTextHandler(os.Stdout, nil)))
			registry.MustRegister(exporter)
			if err := exporter.RefreshOnce(context.Background()); err != nil {
				t.Fatal(err)
			}

			for _, path := range []string{"/wapi/v2.13.7/network", "/wapi/v2.13.7/range"} {
				query, ok := queries[path]
				if !ok {
					t.Fatalf("missing request to %s", path)
				}
				if got := query.Get("network"); got != tt.wantInventoryScope {
					t.Fatalf("%s network scope = %q, want %q", path, got, tt.wantInventoryScope)
				}
			}
			query, ok := queries["/wapi/v2.13.7/ipv4address"]
			if !ok {
				t.Fatal("missing IPv4 address request")
			}
			if got := query.Get("network"); got != "" {
				t.Fatalf("inventory must not use the expensive network expansion filter: %q", got)
			}
			if query.Get("status") != "USED" || query.Get("ip_address>") != "192.0.1.255" || query.Get("ip_address<") != "192.0.3.0" {
				t.Fatalf("unexpected inventory query: %s", query.Encode())
			}

			families, err := registry.Gather()
			if err != nil {
				t.Fatal(err)
			}
			if value := metricValue(t, families, "infoblox_ipv4inventory_collector_configured"); value != 1 {
				t.Fatalf("IPv4 inventory collector configured = %f, want 1", value)
			}
		})
	}
}

func TestIPv4InventoryEmitsOnlySelectedOccupiedAddresses(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wapi/v2.13.7/ipv4address" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		query = r.URL.Query()
		writeResult(t, w, []map[string]interface{}{
			{
				"ip_address":   "198.51.100.17",
				"network":      "198.51.100.1/24",
				"network_view": "default",
				"status":       "USED",
				"names":        []string{"host-b.example", "", "host-a.example", "host-a.example"},
				"types":        []string{"HOST", "HOST"},
				"usage":        []string{"DNS", "DHCP"},
			},
			{"ip_address": "198.51.100.17", "network": "198.51.100.0/24", "network_view": "default", "status": "USED"},
			{"ip_address": "198.51.100.18", "network": "198.51.100.0/24", "network_view": "default", "status": "UNUSED"},
			{"ip_address": "203.0.113.1", "network": "203.0.113.0/24", "network_view": "default", "status": "USED"},
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExcept("ipv4inventory")
	cfg.IPv4InventoryNetworks = []string{"198.51.100.1/24"}
	client, err := wapi.NewClient(wapi.Config{BaseURL: server.URL + "/wapi/v2.13.7", Username: "user", Password: "pass", PageSize: cfg.PageSize})
	if err != nil {
		t.Fatal(err)
	}
	registry := prometheus.NewRegistry()
	exporter := New(cfg, client, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	registry.MustRegister(exporter)
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	if query.Get("status") != "USED" || query.Get("_max_results") != "2000" || !strings.Contains(query.Get("_return_fields"), "names") {
		t.Fatalf("unexpected inventory query: %s", query.Encode())
	}
	info := metricForLabels(t, families, "infoblox_ipv4inventory_info", map[string]string{
		"ip_address": "198.51.100.17", "network": "198.51.100.0/24", "network_view": "default", "status": "USED",
		"names": "host-a.example,host-b.example", "types": "HOST", "usage": "DHCP,DNS",
	})
	if info.GetGauge().GetValue() != 1 || familyMetricCount(families, "infoblox_ipv4inventory_info") != 1 {
		t.Fatalf("unexpected IPv4 inventory info metrics")
	}
	if metricValueForLabels(t, families, "infoblox_ipv4inventory_objects", map[string]string{"stage": "returned"}) != 4 || metricValueForLabels(t, families, "infoblox_ipv4inventory_objects", map[string]string{"stage": "filtered"}) != 2 || metricValueForLabels(t, families, "infoblox_ipv4inventory_objects", map[string]string{"stage": "emitted"}) != 1 {
		t.Fatalf("unexpected IPv4 inventory processing counts")
	}
	if metricValueForLabels(t, families, "infoblox_ipv4inventory_address_count", map[string]string{"network": "198.51.100.0/24", "network_view": "default"}) != 1 {
		t.Fatalf("unexpected IPv4 inventory address count")
	}
}

func TestIPv4InventoryReplacesCache(t *testing.T) {
	empty := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wapi/v2.13.7/ipv4address" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if empty {
			writeResult(t, w, []map[string]interface{}{})
			return
		}
		writeResult(t, w, []map[string]interface{}{
			{
				"ip_address":   "198.51.100.17",
				"network":      "198.51.100.0/24",
				"network_view": "default",
				"status":       "USED",
			},
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExcept("ipv4inventory")
	cfg.IPv4InventoryNetworks = []string{"198.51.100.0/24"}
	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
		Username: "user",
		Password: "pass",
		PageSize: cfg.PageSize,
	})
	if err != nil {
		t.Fatal(err)
	}

	registry := prometheus.NewRegistry()
	exporter := New(cfg, client, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	registry.MustRegister(exporter)
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !hasMetric(families, "infoblox_ipv4inventory_info") {
		t.Fatalf("expected cached IPv4 inventory info")
	}

	empty = true
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	families, err = registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if hasMetric(families, "infoblox_ipv4inventory_info") {
		t.Fatalf("successful empty refresh should remove IPv4 inventory info")
	}
}

func TestExporterHonorsDisabledModules(t *testing.T) {
	requested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = []string{
		"network",
		"range",
		"ipv4inventory",
		"member",
		"restartservicestatus",
		"servicerestart",
		"capacity",
		"license",
		"upgradestatus",
		"dhcpstatistics",
		"ipamstatistics",
		"dhcpfailover",
		"allrecords",
		"zones",
		"dtc",
		"threatprotection",
	}

	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
		Username: "user",
		Password: "pass",
		PageSize: cfg.PageSize,
	})
	if err != nil {
		t.Fatal(err)
	}

	registry := prometheus.NewRegistry()
	exporter := New(cfg, client, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	registry.MustRegister(exporter)
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Gather(); err != nil {
		t.Fatal(err)
	}
	if requested {
		t.Fatalf("disabled collectors should not call WAPI")
	}
}

func TestExporterMetricsReadCacheOnly(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/wapi/v2.13.7/network" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeResult(t, w, []map[string]interface{}{
			{"network": "192.0.2.0/24", "network_view": "default"},
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExcept("network")
	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
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

	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("refresh requests = %d, want 1", requests)
	}
	if _, err := registry.Gather(); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Gather(); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("gather should not call WAPI, requests = %d", requests)
	}
}

func TestExporterKeepsCacheAfterFailedRefresh(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wapi/v2.13.7/network" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if fail {
			http.Error(w, "failed", http.StatusInternalServerError)
			return
		}
		writeResult(t, w, []map[string]interface{}{
			{"network": "192.0.2.0/24", "network_view": "default"},
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExcept("network")
	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
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

	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := exporter.RefreshOnce(context.Background()); err == nil {
		t.Fatalf("expected refresh failure")
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if value := metricValue(t, families, "infoblox_network_info"); value != 1 {
		t.Fatalf("cached network metric = %f, want 1", value)
	}
	if value := metricValue(t, families, "infoblox_up"); value != 0 {
		t.Fatalf("failed refresh should set infoblox_up = 0, got %f", value)
	}
}

func TestExporterPublishesCacheAfterOptionalCollectorFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wapi/v2.13.7/network":
			writeResult(t, w, []map[string]interface{}{
				{"network": "192.0.2.0/24", "network_view": "default"},
			})
		case "/wapi/v2.13.7/threatprotection:statistics":
			http.Error(w, "slow collector failed", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExceptAny("network", "threatprotection")
	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
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

	if err := exporter.RefreshOnce(context.Background()); err == nil {
		t.Fatalf("expected degraded refresh error")
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if value := metricValue(t, families, "infoblox_network_info"); value != 1 {
		t.Fatalf("network metric after degraded refresh = %f, want 1", value)
	}
	if value := metricValue(t, families, "infoblox_up"); value != 0 {
		t.Fatalf("degraded refresh should set infoblox_up = 0, got %f", value)
	}
}

func TestExporterReplacesCacheAfterSuccessfulRefresh(t *testing.T) {
	empty := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wapi/v2.13.7/network" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if empty {
			writeResult(t, w, []map[string]interface{}{})
			return
		}
		writeResult(t, w, []map[string]interface{}{
			{"network": "192.0.2.0/24", "network_view": "default"},
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.DisabledModules = allModulesExcept("network")
	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
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

	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !hasMetric(families, "infoblox_network_info") {
		t.Fatalf("expected cached network metric")
	}

	empty = true
	if err := exporter.RefreshOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	families, err = registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if hasMetric(families, "infoblox_network_info") {
		t.Fatalf("successful empty refresh should replace previous network metric")
	}
}

func allModulesExcept(keep string) []string {
	return allModulesExceptAny(keep)
}

func allModulesExceptAny(keep ...string) []string {
	modules := []string{
		"network",
		"range",
		"ipv4inventory",
		"member",
		"restartservicestatus",
		"servicerestart",
		"capacity",
		"license",
		"upgradestatus",
		"dhcpstatistics",
		"ipamstatistics",
		"dhcpfailover",
		"allrecords",
		"zones",
		"dtc",
		"threatprotection",
	}
	kept := map[string]bool{}
	for _, module := range keep {
		kept[module] = true
	}
	out := make([]string, 0, len(modules)-len(kept))
	for _, module := range modules {
		if !kept[module] {
			out = append(out, module)
		}
	}
	return out
}

func hasMetric(families []*dto.MetricFamily, name string) bool {
	for _, family := range families {
		if family.GetName() == name && len(family.Metric) > 0 {
			return true
		}
	}
	return false
}

func familyMetricCount(families []*dto.MetricFamily, name string) int {
	for _, family := range families {
		if family.GetName() == name {
			return len(family.Metric)
		}
	}
	return 0
}

func metricForLabels(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != len(labels) {
				continue
			}
			matches := true
			for _, pair := range metric.Label {
				value, ok := labels[pair.GetName()]
				if !ok || value != pair.GetValue() {
					matches = false
					break
				}
			}
			if matches {
				return metric
			}
		}
	}
	t.Fatalf("missing metric %s with labels %#v", name, labels)
	return nil
}

func metricValueForLabels(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	return metricForLabels(t, families, name, labels).GetGauge().GetValue()
}

func writeResult(t *testing.T, w http.ResponseWriter, result interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"result": result}); err != nil {
		t.Fatal(err)
	}
}

func writeObject(t *testing.T, w http.ResponseWriter, result interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(result); err != nil {
		t.Fatal(err)
	}
}
