# infoblox-exporter

[![CI](https://github.com/elohmeier/infoblox-exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/elohmeier/infoblox-exporter/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/elohmeier/infoblox-exporter)](https://github.com/elohmeier/infoblox-exporter/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/elohmeier/infoblox-exporter)](https://goreportcard.com/report/github.com/elohmeier/infoblox-exporter)
[![Go Reference](https://pkg.go.dev/badge/github.com/elohmeier/infoblox-exporter.svg)](https://pkg.go.dev/github.com/elohmeier/infoblox-exporter)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Prometheus exporter for selected Infoblox NIOS WAPI inventory and utilization data.

The exporter uses read-only WAPI requests with paging enabled. A background scheduler refreshes data into an in-process cache, and Prometheus scrapes read that cache only. The bounded `ipv4inventory` collector streams occupied address records page by page instead of expanding every address in each network. DNS `allrecords` are exported both as aggregate counts and per-record info/TTL metrics.

## Quick Start

```sh
export INFOBLOX_USERNAME='<readonly-user>'
export INFOBLOX_PASSWORD='<password>'
go run . -url https://gm.example.com/wapi/v2.13.7 \
  -networks 192.0.2.0/24 \
  -ipv4-inventory-networks 192.0.2.0/24
```

The exporter listens on `:9717` and exposes `/metrics`, `/health`, `/readyz`, and `/debug/cache`.

```sh
curl http://localhost:9717/metrics
```

## Metrics

Core metrics include:

- `infoblox_up`
- `infoblox_scrape_duration_seconds`
- `infoblox_refresh_duration_seconds`
- `infoblox_cache_age_seconds`
- `infoblox_cache_stale`
- `infoblox_wapi_requests_total{object,code}`
- `infoblox_wapi_request_duration_seconds{object}`
- `infoblox_network_utilization_ratio{network,network_view}`
- `infoblox_network_dhcp_utilization_ratio{network,network_view}`
- `infoblox_range_dhcp_utilization_ratio{network,network_view,start_addr,end_addr}`
- `infoblox_ipv4inventory_info{ip_address,network,network_view,status,names,types,usage}`
- `infoblox_ipv4inventory_address_count{network,network_view}`
- `infoblox_ipv4inventory_collector_configured`
- `infoblox_ipv4inventory_objects{stage}`
- `infoblox_ipv4inventory_selected_networks`
- `infoblox_ipv4inventory_scan_ranges`
- `infoblox_member_service_status{member,service,status}`
- `infoblox_restart_service_status{member,service,status}`
- `infoblox_service_restart_status_count{parent,grouped,state}`
- `infoblox_capacity_used_ratio{member,role,hardware_type}`
- `infoblox_license_info{scope,type,kind,limit,limit_context,expiration_status,hwid}`
- `infoblox_upgrade_status_info{type,member,upgrade_group,...}`
- `infoblox_dhcp_statistics_utilization_ratio{object_type,object}`
- `infoblox_ipam_statistics_utilization_ratio{network,network_view}`
- `infoblox_dhcp_failover_info{name,association_type,...}`
- `infoblox_dns_record_info{view,zone,type,name,disabled,reclaimable}`
- `infoblox_dns_record_ttl_seconds{view,zone,type,name}`
- `infoblox_dns_record_count{view,zone,type}`
- `infoblox_dns_zone_info{view,zone,type,comment,disabled}`
- `infoblox_dtc_object_info{name,abstract_type,display_type,status,comment}`
- `infoblox_threatprotection_stat_value{member,stat}`

## Configuration

Flags follow the same style as the neighboring NetScaler exporter:

| Flag | Environment | Default | Description |
| --- | --- | --- | --- |
| `-url` | `INFOBLOX_URL` | required | Infoblox WAPI base URL, for example `https://gm.example.com/wapi/v2.13.7`. `INFOBLOX_WAPI_URL` and `INFOBLOX_BASE_URL` are also accepted. |
| `-labels` | `INFOBLOX_LABELS` | none | Comma-separated Prometheus const labels, for example `env=prod,dc=de`. CLI labels override env labels with the same key. |
| `-disabled-modules` | `INFOBLOX_DISABLED_MODULES` | none | Comma-separated collectors to disable. |
| `-bind-port` | none | `9717` | HTTP port for `/metrics`, `/health`, `/readyz`, and `/debug/cache`. |
| `-page-size` | `INFOBLOX_PAGE_SIZE` | `1000` | WAPI paging size. `INFOBLOX_EXPORTER_PAGE_SIZE` is also accepted. |
| `-timeout` | `INFOBLOX_TIMEOUT` | `30s` | WAPI request timeout. `INFOBLOX_EXPORTER_TIMEOUT` is also accepted. |
| `-refresh-interval` | `INFOBLOX_REFRESH_INTERVAL` | `5m` | Background cache refresh interval. |
| `-refresh-timeout` | `INFOBLOX_REFRESH_TIMEOUT` | `2m` | Timeout for one full background refresh. |
| `-max-stale` | `INFOBLOX_MAX_STALE` | `15m` | Maximum cache age before `/readyz` reports not ready. |
| `-ignore-cert` | `INFOBLOX_IGNORE_CERT` | `false` | Disable TLS certificate verification. `INFOBLOX_EXPORTER_INSECURE_SKIP_VERIFY` is also accepted. |
| `-ca-file` | `INFOBLOX_CA_FILE` | none | Custom CA bundle path. |
| `-network-views` | `INFOBLOX_NETWORK_VIEWS` | all | Comma-separated network views for IPAM/DHCP collectors. |
| `-dns-views` | `INFOBLOX_DNS_VIEWS` | all | Comma-separated DNS views. |
| `-networks` | `INFOBLOX_NETWORKS` | none | Comma-separated CIDRs for network, range, DHCP statistics, and IPAM statistics collectors. |
| `-ipv4-inventory-networks` | `INFOBLOX_IPV4_INVENTORY_NETWORKS` | none | Comma-separated network CIDRs selected for IPv4 inventory. |
| `-ipv4-inventory-scan-ranges` | `INFOBLOX_IPV4_INVENTORY_SCAN_RANGES` | none | Optional CIDRs used as broader WAPI address query intervals. Selected networks are still filtered exactly in the exporter. |
| `-ipv4-inventory-name-regex` | `INFOBLOX_IPV4_INVENTORY_NAME_REGEX` | none | Optional WAPI regular expression applied to the `names` field. |
| `-ipv4-inventory-network-ea` | `INFOBLOX_IPV4_INVENTORY_NETWORK_EA` | none | Select networks by extensible attribute in `name=value` form. |
| `-ipv4-inventory-page-size` | `INFOBLOX_IPV4_INVENTORY_PAGE_SIZE` | `2000` | WAPI page size used by network discovery and address inventory reads. |
| `-ipv4-inventory-max-addresses` | `INFOBLOX_IPV4_INVENTORY_MAX_ADDRESSES` | `100000` | Hard limit on unique occupied addresses retained by a refresh. Exceeding it fails the refresh and preserves the previous cache. |
| `-ipv4-inventory-timeout` | `INFOBLOX_IPV4_INVENTORY_TIMEOUT` | `5m` | Collector-specific timeout for IPv4 inventory. The full `-refresh-timeout` still applies. |
| `-zones` | `INFOBLOX_ZONES` | none | Comma-separated DNS zones for `allrecords` and `zones`. |
| `-upgrade-status-types` | `INFOBLOX_UPGRADE_STATUS_TYPES` | `GRID,GROUP,VNODE,PNODE` | Upgrade status object types to query. |

Credentials are read from `INFOBLOX_USERNAME` and `INFOBLOX_PASSWORD`.

`/metrics` returns HTTP 200 even before the first successful refresh, but it only exposes exporter/cache self-metrics until cached Infoblox data exists. Use `/readyz` for readiness; it returns HTTP 503 until the cache has been refreshed successfully and is not stale.

Disable collectors by these names: `network`, `range`, `ipv4inventory`, `member`, `restartservicestatus`, `servicerestart`, `capacity`, `license`, `upgradestatus`, `dhcpstatistics`, `ipamstatistics`, `dhcpfailover`, `allrecords`, `zones`, `dtc`, `threatprotection`.

## Collector Scope

The `network`, `range`, and `member` collectors can query all objects in the configured network views. If `-networks` is set, network and range collection is restricted to those CIDRs.

The `ipv4inventory` collector remains inactive unless at least one inventory network, scan range, name regex, or network EA selector is configured. There is no accidental empty global scrape.

Inventory requests always apply `status=USED`, address bounds, and the optional name regex in WAPI. Explicit networks and EA-selected networks are normalized, overlapping or adjacent CIDRs are merged into scan intervals, and returned records are then filtered against the exact selected network/view set. Pages are processed immediately; only the compact deduplicated result is cached. The `names`, `types`, and `usage` arrays are sorted, deduplicated, and joined with commas.

When `-ipv4-inventory-scan-ranges` is set, those ranges control the WAPI query intervals while `-ipv4-inventory-networks` and `-ipv4-inventory-network-ea` remain exact local selectors. Scan ranges alone inventory every occupied address in those ranges. A name regex alone is allowed to perform an unbounded name-filtered search; the address cap remains mandatory protection.

### Migrating IPv4 collector configuration

The old `ipv4address` collector, `-ipv4-networks`, `INFOBLOX_IPV4_NETWORKS`, `-ipv4-address-info`, `INFOBLOX_IPV4_ADDRESS_INFO`, and all `infoblox_ipv4address_*` metrics have been removed. Move the desired CIDRs to `-ipv4-inventory-networks` or `INFOBLOX_IPV4_INVENTORY_NETWORKS`. Keep `-networks` as well when the network, range, DHCP statistics, and IPAM statistics collectors should remain restricted to the same CIDRs.

The `allrecords` collector emits one `infoblox_dns_record_info` metric per DNS record plus aggregate counts. It requires explicit `-zones` entries because WAPI requires a zone search parameter for allrecords searches. Use `-dns-views` when you need to restrict DNS views.

WAPI-accessible operational collectors include restart service status, service restart requests, capacity reports, licenses, upgrade status, DHCP statistics, IPAM statistics, DHCP failover, DTC object state, and threat protection numeric statistics.

## Releases

Version tags matching `v*.*.*` create a GitHub Release with checksums, source archive, and Linux/macOS binaries for `amd64` and `arm64`.

Container images are published to GHCR:

```sh
docker pull ghcr.io/elohmeier/infoblox-exporter:latest
docker run --rm -p 9717:9717 \
  -e INFOBLOX_USERNAME='<readonly-user>' \
  -e INFOBLOX_PASSWORD='<password>' \
  ghcr.io/elohmeier/infoblox-exporter:latest \
  -url https://gm.example.com/wapi/v2.13.7 \
  -networks 192.0.2.0/24 \
  -ipv4-inventory-networks 192.0.2.0/24
```

## Build

```sh
make ci
```

Useful individual targets:

```sh
make fmt
make test-cover
make docker
```
