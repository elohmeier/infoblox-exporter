package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elohmeier/infoblox-exporter/internal/config"
	"github.com/elohmeier/infoblox-exporter/internal/model"
	"github.com/elohmeier/infoblox-exporter/internal/wapi"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCollectReportsFailure(t *testing.T) {
	cfg := config.Default()
	cfg.DisabledModules = []string{
		"range", "ipv4address", "member", "restartservicestatus", "servicerestart",
		"capacity", "license", "upgradestatus", "dhcpstatistics", "ipamstatistics",
		"dhcpfailover", "allrecords", "zones", "dtc", "threatprotection",
	}
	exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	})
	defer cleanup()

	registry := prometheus.NewRegistry()
	registry.MustRegister(exporter)
	if err := exporter.RefreshOnce(context.Background()); err == nil {
		t.Fatalf("expected refresh failure")
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if value := metricValue(t, families, "infoblox_up"); value != 0 {
		t.Fatalf("expected failed scrape, got %f", value)
	}
}

func TestCollectIPv4Unconfigured(t *testing.T) {
	cfg := config.Default()
	cfg.DisabledModules = []string{
		"network", "range", "member", "restartservicestatus", "servicerestart",
		"capacity", "license", "upgradestatus", "dhcpstatistics", "ipamstatistics",
		"dhcpfailover", "allrecords", "zones", "dtc", "threatprotection",
	}
	exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	})
	defer cleanup()

	registry := prometheus.NewRegistry()
	registry.MustRegister(exporter)
	if _, err := registry.Gather(); err != nil {
		t.Fatal(err)
	}
}

func TestRunCollectorUsesRefreshContext(t *testing.T) {
	exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	})
	defer cleanup()

	parent, cancel := context.WithCancel(context.Background())
	cancel()

	if got := exporter.runCollector(parent, "test", func(ctx context.Context, _ chan<- prometheus.Metric) error {
		return ctx.Err()
	}); !got.failed {
		t.Fatalf("collector should inherit canceled refresh context")
	}
}

func TestCollectorPrimaryErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		cfg  func() config.Config
		call func(context.Context, *Exporter, chan prometheus.Metric) error
	}{
		{name: "network", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectNetworks(ctx, ch)
		}},
		{name: "range", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectRanges(ctx, ch)
		}},
		{
			name: "ipv4address",
			cfg: func() config.Config {
				cfg := config.Default()
				cfg.Networks = []string{"10.0.0.0/24"}
				return cfg
			},
			call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
				return e.collectIPv4Addresses(ctx, ch)
			},
		},
		{name: "member", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectMembers(ctx, ch)
		}},
		{name: "restartservicestatus", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectRestartServiceStatus(ctx, ch)
		}},
		{name: "servicerestart", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectServiceRestart(ctx, ch)
		}},
		{name: "capacity", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectCapacity(ctx, ch)
		}},
		{name: "license", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectLicenses(ctx, ch)
		}},
		{name: "upgradestatus", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectUpgradeStatus(ctx, ch)
		}},
		{name: "dhcpstatistics", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectDHCPStatistics(ctx, ch)
		}},
		{name: "ipamstatistics", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectIPAMStatistics(ctx, ch)
		}},
		{name: "dhcpfailover", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectDHCPFailover(ctx, ch)
		}},
		{
			name: "allrecords",
			cfg: func() config.Config {
				cfg := config.Default()
				cfg.Zones = []string{"example.test"}
				return cfg
			},
			call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
				return e.collectAllRecords(ctx, ch)
			},
		},
		{name: "zones", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectZones(ctx, ch)
		}},
		{name: "dtc", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error { return e.collectDTC(ctx, ch) }},
		{name: "threatprotection", call: func(ctx context.Context, e *Exporter, ch chan prometheus.Metric) error {
			return e.collectThreatProtection(ctx, ch)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			if tt.cfg != nil {
				cfg = tt.cfg()
			}
			exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "failed", http.StatusInternalServerError)
			})
			defer cleanup()
			if err := tt.call(context.Background(), exporter, metricBuffer()); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestCollectorScopedErrorPaths(t *testing.T) {
	t.Run("network", func(t *testing.T) {
		cfg := config.Default()
		cfg.Networks = []string{"10.0.0.0/24"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "failed", http.StatusInternalServerError)
		})
		defer cleanup()
		if err := exporter.collectNetworks(context.Background(), metricBuffer()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("range", func(t *testing.T) {
		cfg := config.Default()
		cfg.Networks = []string{"10.0.0.0/24"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "failed", http.StatusInternalServerError)
		})
		defer cleanup()
		if err := exporter.collectRanges(context.Background(), metricBuffer()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("dhcp network refs", func(t *testing.T) {
		cfg := config.Default()
		cfg.Networks = []string{"10.0.0.0/24"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "failed", http.StatusInternalServerError)
		})
		defer cleanup()
		if _, err := exporter.dhcpStatisticsObjects(context.Background()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("dhcp range refs", func(t *testing.T) {
		cfg := config.Default()
		cfg.Networks = []string{"10.0.0.0/24"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/network":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/range":
				http.Error(w, "failed", http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if _, err := exporter.dhcpStatisticsObjects(context.Background()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("ipam", func(t *testing.T) {
		cfg := config.Default()
		cfg.Networks = []string{"10.0.0.0/24"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "failed", http.StatusInternalServerError)
		})
		defer cleanup()
		if err := exporter.collectIPAMStatistics(context.Background(), metricBuffer()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("allrecords", func(t *testing.T) {
		cfg := config.Default()
		cfg.Zones = []string{"example.test"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "failed", http.StatusInternalServerError)
		})
		defer cleanup()
		if err := exporter.collectAllRecords(context.Background(), metricBuffer()); err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestCollectorSecondaryErrorPaths(t *testing.T) {
	t.Run("service restart request", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/grid:servicerestart:status":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/grid:servicerestart:request":
				http.Error(w, "failed", http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectServiceRestart(context.Background(), metricBuffer()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("grid license", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/member:license":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/license:gridwide":
				http.Error(w, "failed", http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectLicenses(context.Background(), metricBuffer()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("dhcp range refs", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/network":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/range":
				http.Error(w, "failed", http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if _, err := exporter.dhcpStatisticsObjects(context.Background()); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("dhcp statistics object", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/network":
				writeResult(t, w, []map[string]interface{}{{"_ref": "network/ref", "network": "10.0.0.0/24"}})
			case "/wapi/v2.13.7/range":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/dhcp:statistics":
				http.Error(w, "failed", http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectDHCPStatistics(context.Background(), metricBuffer()); err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestCollectorsWithoutNetworkScope(t *testing.T) {
	exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wapi/v2.13.7/network":
			writeResult(t, w, []map[string]interface{}{{"_ref": "network/ref", "network": "10.0.0.0/24"}})
		case "/wapi/v2.13.7/range":
			writeResult(t, w, []map[string]interface{}{{"_ref": "range/ref", "network": "10.0.0.0/24"}})
		case "/wapi/v2.13.7/ipam:statistics":
			writeObject(t, w, map[string]interface{}{"network": "10.0.0.0/24", "network_view": "default"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})
	defer cleanup()

	if err := exporter.collectNetworks(context.Background(), metricBuffer()); err != nil {
		t.Fatal(err)
	}
	if err := exporter.collectRanges(context.Background(), metricBuffer()); err != nil {
		t.Fatal(err)
	}
	if _, err := exporter.dhcpStatisticsObjects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := exporter.collectIPAMStatistics(context.Background(), metricBuffer()); err != nil {
		t.Fatal(err)
	}
}

func TestCollectorScopedBranches(t *testing.T) {
	t.Run("capacity is scoped by member name", func(t *testing.T) {
		seenCapacity := false
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/member":
				writeResult(t, w, []map[string]interface{}{{"host_name": "member-a"}})
			case "/wapi/v2.13.7/capacityreport":
				seenCapacity = true
				if got := r.URL.Query().Get("name"); got != "member-a" {
					t.Fatalf("capacityreport missing member name: %q", got)
				}
				writeResult(t, w, []map[string]interface{}{})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectCapacity(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
		if !seenCapacity {
			t.Fatalf("capacityreport was not queried")
		}
	})

	t.Run("capacity falls back to wapi hostname", func(t *testing.T) {
		seenCapacity := false
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/member":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/capacityreport":
				seenCapacity = true
				if got := r.URL.Query().Get("name"); got != "127.0.0.1" {
					t.Fatalf("capacityreport missing fallback host name: %q", got)
				}
				writeResult(t, w, []map[string]interface{}{
					{
						"name":          "127.0.0.1",
						"object_counts": []map[string]interface{}{{"type_name": "Grid Member", "count": 37}},
					},
				})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectCapacity(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
		if !seenCapacity {
			t.Fatalf("capacityreport was not queried")
		}
	})

	t.Run("capacity skips GMC scoped member errors", func(t *testing.T) {
		seenCapacity := false
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/member":
				writeResult(t, w, []map[string]interface{}{{"host_name": "member-a"}})
			case "/wapi/v2.13.7/capacityreport":
				seenCapacity = true
				w.WriteHeader(http.StatusBadRequest)
				writeObject(t, w, map[string]interface{}{
					"code": "Client.Ibap.Data",
					"text": "GMC can only retrieve the capacity report for itself, not for other physical nodes.",
				})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectCapacity(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
		if !seenCapacity {
			t.Fatalf("capacityreport was not queried")
		}
	})

	t.Run("license omits unsupported fields", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/member:license":
				if fields := r.URL.Query().Get("_return_fields"); strings.Contains(fields, "hwid") {
					t.Fatalf("member license requested unsupported hwid field: %s", fields)
				}
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/license:gridwide":
				if fields := r.URL.Query().Get("_return_fields"); strings.Contains(fields, "kind") {
					t.Fatalf("gridwide license requested unsupported kind field: %s", fields)
				}
				if fields := r.URL.Query().Get("_return_fields"); strings.Contains(fields, "hwid") {
					t.Fatalf("gridwide license requested unsupported hwid field: %s", fields)
				}
				writeResult(t, w, []map[string]interface{}{})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectLicenses(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("dhcp statistics reference reads are not paged", func(t *testing.T) {
		cfg := config.Default()
		cfg.Networks = []string{"10.0.0.0/24"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/network":
				writeResult(t, w, []map[string]interface{}{{"_ref": "network/ref", "network": "10.0.0.0/24"}})
			case "/wapi/v2.13.7/range":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/dhcp:statistics":
				if got := r.URL.Query().Get("_paging"); got != "" {
					t.Fatalf("dhcp statistics reference read should not use paging: %q", got)
				}
				writeObject(t, w, map[string]interface{}{})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectDHCPStatistics(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("dhcp statistics skips timed out objects", func(t *testing.T) {
		cfg := config.Default()
		cfg.Timeout = 60 * time.Millisecond
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/network":
				writeResult(t, w, []map[string]interface{}{{"_ref": "network/slow", "network": "10.0.0.0/24"}})
			case "/wapi/v2.13.7/range":
				writeResult(t, w, []map[string]interface{}{{"_ref": "range/fast", "network": "10.0.0.0/24"}})
			case "/wapi/v2.13.7/dhcp:statistics":
				if r.URL.Query().Get("statistics_object") == "network/slow" {
					time.Sleep(90 * time.Millisecond)
				}
				writeObject(t, w, map[string]interface{}{})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectDHCPStatistics(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("dhcp statistics stops cleanly at collector deadline", func(t *testing.T) {
		cfg := config.Default()
		cfg.Timeout = 30 * time.Millisecond
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wapi/v2.13.7/network":
				networks := make([]map[string]interface{}, 0, 20)
				for i := 0; i < 20; i++ {
					networks = append(networks, map[string]interface{}{
						"_ref":    fmt.Sprintf("network/slow-%d", i),
						"network": fmt.Sprintf("10.0.%d.0/24", i),
					})
				}
				writeResult(t, w, networks)
			case "/wapi/v2.13.7/range":
				writeResult(t, w, []map[string]interface{}{})
			case "/wapi/v2.13.7/dhcp:statistics":
				time.Sleep(20 * time.Millisecond)
				writeObject(t, w, map[string]interface{}{})
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		})
		defer cleanup()
		if err := exporter.collectDHCPStatistics(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ipam statistics omits fields unavailable on containers", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			if fields := r.URL.Query().Get("_return_fields"); strings.Contains(fields, "conflict_count") {
				t.Fatalf("ipam statistics requested unavailable conflict_count field: %s", fields)
			}
			if fields := r.URL.Query().Get("_return_fields"); strings.Contains(fields, "unmanaged_count") {
				t.Fatalf("ipam statistics requested unavailable unmanaged_count field: %s", fields)
			}
			if fields := r.URL.Query().Get("_return_fields"); strings.Contains(fields, "utilization_update") {
				t.Fatalf("ipam statistics requested unavailable utilization_update field: %s", fields)
			}
			writeObject(t, w, map[string]interface{}{})
		})
		defer cleanup()
		if err := exporter.collectIPAMStatistics(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("allrecords skips unscoped collection", func(t *testing.T) {
		requested := false
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, _ *http.Request) {
			requested = true
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		})
		defer cleanup()
		if err := exporter.collectAllRecords(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
		if requested {
			t.Fatalf("allrecords should not query without configured zones")
		}
	})

	t.Run("zones does not assume default dns view", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("view"); got != "" {
				t.Fatalf("unexpected default view filter: %q", got)
			}
			writeResult(t, w, []map[string]interface{}{})
		})
		defer cleanup()
		if err := exporter.collectZonesForObject(context.Background(), metricBuffer(), "zone_auth"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("allrecords zones", func(t *testing.T) {
		cfg := config.Default()
		cfg.Zones = []string{"example.test"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("zone") != "example.test" {
				t.Fatalf("unexpected zone: %s", r.URL.Query().Get("zone"))
			}
			writeResult(t, w, []map[string]interface{}{})
		})
		defer cleanup()
		if err := exporter.collectAllRecords(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("zone filter", func(t *testing.T) {
		cfg := config.Default()
		cfg.Zones = []string{"match.test"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
			writeResult(t, w, []map[string]interface{}{
				{"fqdn": "skip.test", "view": "default"},
				{"fqdn": "match.test", "view": "default"},
			})
		})
		defer cleanup()
		if err := exporter.collectZonesForObject(context.Background(), metricBuffer(), "zone_auth"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCollectorSuccessBranches(t *testing.T) {
	t.Run("ipv4 empty types", func(t *testing.T) {
		cfg := config.Default()
		cfg.Networks = []string{"10.0.0.0/24"}
		exporter, cleanup := newCoverageExporter(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
			writeResult(t, w, []map[string]interface{}{
				{"ip_address": "10.0.0.1", "status": "USED"},
			})
		})
		defer cleanup()
		if err := exporter.collectIPv4Addresses(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("members with skipped and enabled statuses", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, _ *http.Request) {
			writeResult(t, w, []map[string]interface{}{
				{
					"host_name": "member-a",
					"service_status": []map[string]interface{}{
						{},
						{"service": "DHCP", "enabled": true},
					},
				},
			})
		})
		defer cleanup()
		if err := exporter.collectMembers(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("capacity skips invalid object counts", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, _ *http.Request) {
			writeResult(t, w, []map[string]interface{}{
				{
					"name":          "member-a",
					"object_counts": []map[string]interface{}{{"type": ""}, {"type": "network", "count": "bad"}},
				},
			})
		})
		defer cleanup()
		if err := exporter.collectCapacity(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("threat protection default member", func(t *testing.T) {
		exporter, cleanup := newCoverageExporter(t, config.Default(), func(w http.ResponseWriter, _ *http.Request) {
			writeResult(t, w, []map[string]interface{}{
				{
					"stat_infos": []map[string]interface{}{
						{"events": []interface{}{float64(1), float64(2)}},
					},
				},
			})
		})
		defer cleanup()
		if err := exporter.collectThreatProtection(context.Background(), metricBuffer()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCollectorHelpers(t *testing.T) {
	if !reflect.DeepEqual(viewsOrSingleEmpty(nil), []string{""}) {
		t.Fatalf("unexpected empty view fallback")
	}
	if !reflect.DeepEqual(viewsOrSingleEmpty([]string{"default"}), []string{"default"}) {
		t.Fatalf("unexpected explicit views")
	}
	if valueOr("", "") != "" || valueOr("", "x") != "x" {
		t.Fatalf("unexpected valueOr result")
	}

	statusGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_status", Help: "test"}, []string{"a", "b"})
	emitStatus(statusGauge, "", "b")

	recordGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_record", Help: "test"}, []string{"a", "b", "c"})
	emitRecordCounts(recordGauge, map[string]int{"single": 1})

	if number, ok := firstNumber(map[string]interface{}{"a": "bad", "b": float64(2)}, "missing", "a", "b"); !ok || number != 2 {
		t.Fatalf("unexpected first number: %f %t", number, ok)
	}
	if _, ok := firstNumber(map[string]interface{}{"a": struct{}{}}, "a"); ok {
		t.Fatalf("unexpected number")
	}

	numberCases := []interface{}{
		float64(1), float32(2), int(3), int64(4), uint64(5), json.Number("6"), "7",
	}
	for _, value := range numberCases {
		if _, ok := numberFromAny(value); !ok {
			t.Fatalf("expected number from %#v", value)
		}
	}
	for _, value := range []interface{}{json.Number("bad"), "bad", struct{}{}} {
		if _, ok := numberFromAny(value); ok {
			t.Fatalf("unexpected number from %#v", value)
		}
	}

	out := map[string]float64{}
	collectNumericLeaves("", float64(1), out)
	collectNumericLeaves("root", map[string]interface{}{"b": float64(2), "a": map[string]interface{}{"c": float64(3)}}, out)
	collectNumericLeaves("list", []interface{}{float64(4), map[string]interface{}{"nested": float64(5)}}, out)
	collectNumericLeaves("ignored", true, out)
	if out["value"] != 1 || out["root.b"] != 2 || out["root.a.c"] != 3 || out["list"] != 4 || out["list.nested"] != 5 {
		t.Fatalf("unexpected numeric leaves: %#v", out)
	}

	if !contains([]string{"a", "b"}, "b") || contains([]string{"a"}, "b") {
		t.Fatalf("unexpected contains result")
	}
	for _, tt := range []struct {
		value uint64
		want  float64
	}{
		{968, 0.968},
		{1000, 1},
		{500, 0.5},
	} {
		if got := ipamUtilizationRatio(tt.value); got != tt.want {
			t.Fatalf("ipamUtilizationRatio(%d) = %f, want %f", tt.value, got, tt.want)
		}
	}
	for _, tt := range []struct {
		value uint64
		want  float64
	}{
		{23, 0.023},
		{100, 0.1},
		{1000, 1},
		{250, 0.25},
	} {
		if got := dhcpUtilizationRatio(tt.value); got != tt.want {
			t.Fatalf("dhcpUtilizationRatio(%d) = %f, want %f", tt.value, got, tt.want)
		}
	}
	if service, status := memberServiceStatus(map[string]interface{}{"name": stringer("DNS"), "enabled": false}); service != "DNS" || status != "enabled_false" {
		t.Fatalf("unexpected member service status: %s/%s", service, status)
	}
	if got := firstString(map[string]interface{}{"a": "", "b": emptyStringer{}, "c": stringer("value")}, "a", "b", "c"); got != "value" {
		t.Fatalf("unexpected first string: %s", got)
	}
	if got := firstString(map[string]interface{}{"a": 1}, "a"); got != "" {
		t.Fatalf("unexpected first string: %s", got)
	}

	if objects := networkStatisticsObjects([]model.Network{{}, {Ref: "network/ref", Network: "10.0.0.0/24"}}); len(objects) != 1 {
		t.Fatalf("unexpected network objects: %#v", objects)
	}
	if objects := rangeStatisticsObjects([]model.Range{{}, {Ref: "range/ref", Network: "10.0.0.0/24"}}); len(objects) != 1 || objects[0].name != "10.0.0.0/24" {
		t.Fatalf("unexpected range objects: %#v", objects)
	}
}

type stringer string

func (s stringer) String() string {
	return string(s)
}

type emptyStringer struct{}

func (emptyStringer) String() string {
	return ""
}

func newCoverageExporter(t *testing.T, cfg config.Config, handler http.HandlerFunc) (*Exporter, func()) {
	t.Helper()
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Second
	}
	if cfg.PageSize == 0 {
		cfg.PageSize = 1000
	}
	server := httptest.NewServer(handler)
	client, err := wapi.NewClient(wapi.Config{
		BaseURL:  server.URL + "/wapi/v2.13.7",
		Username: "user",
		Password: "pass",
		PageSize: cfg.PageSize,
		Timeout:  cfg.Timeout,
	})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, client, logger), server.Close
}

func metricBuffer() chan prometheus.Metric {
	return make(chan prometheus.Metric, 1000)
}

func metricValue(t *testing.T, families []*dto.MetricFamily, name string) float64 {
	t.Helper()
	for _, family := range families {
		if family.GetName() != name || len(family.Metric) == 0 {
			continue
		}
		metric := family.Metric[0]
		if metric.Gauge != nil {
			return metric.Gauge.GetValue()
		}
		if metric.Counter != nil {
			return metric.Counter.GetValue()
		}
	}
	t.Fatalf("missing metric: %s", name)
	return 0
}
