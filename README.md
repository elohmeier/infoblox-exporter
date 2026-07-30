# infoblox-exporter

[![CI](https://github.com/elohmeier/infoblox-exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/elohmeier/infoblox-exporter/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/elohmeier/infoblox-exporter)](https://github.com/elohmeier/infoblox-exporter/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/elohmeier/infoblox-exporter)](https://goreportcard.com/report/github.com/elohmeier/infoblox-exporter)
[![Go Reference](https://pkg.go.dev/badge/github.com/elohmeier/infoblox-exporter.svg)](https://pkg.go.dev/github.com/elohmeier/infoblox-exporter)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Prometheus exporter for selected Infoblox NIOS WAPI inventory and utilization data.

The exporter uses read-only WAPI requests with paging enabled. A background scheduler refreshes data into an in-process cache, and Prometheus scrapes read that cache only. IPv4 address data is aggregated by network/view/status/type/usage by default; an explicit opt-in can additionally expose metadata for occupied addresses. DNS `allrecords` are exported both as aggregate counts and per-record info/TTL metrics.

## Quick Start

```sh
export INFOBLOX_USERNAME='<readonly-user>'
export INFOBLOX_PASSWORD='<password>'
go run . -url https://gm.example.com/wapi/v2.13.7 \
  -networks 10.1.216.0/24 \
  -ipv4-networks 10.1.216.0/24
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
- `infoblox_ipv4address_status_count{network,network_view,status}`
- `infoblox_ipv4address_type_count{network,network_view,type}`
- `infoblox_ipv4address_usage_count{network,network_view,usage}`
- `infoblox_ipv4address_info{ip_address,network,network_view,status,names,types,usage}` (opt-in)
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
| `-ipv4-networks` | `INFOBLOX_IPV4_NETWORKS` | none | Comma-separated CIDRs for the IPv4 address collector. |
| `-ipv4-address-info` | `INFOBLOX_IPV4_ADDRESS_INFO` | `false` | Expose a high-cardinality `infoblox_ipv4address_info` series for every occupied address in the configured IPv4 networks. |
| `-zones` | `INFOBLOX_ZONES` | none | Comma-separated DNS zones for `allrecords` and `zones`. |
| `-upgrade-status-types` | `INFOBLOX_UPGRADE_STATUS_TYPES` | `GRID,GROUP,VNODE,PNODE` | Upgrade status object types to query. |

Credentials are read from `INFOBLOX_USERNAME` and `INFOBLOX_PASSWORD`.

`/metrics` returns HTTP 200 even before the first successful refresh, but it only exposes exporter/cache self-metrics until cached Infoblox data exists. Use `/readyz` for readiness; it returns HTTP 503 until the cache has been refreshed successfully and is not stale.

Disable collectors by these names: `network`, `range`, `ipv4address`, `member`, `restartservicestatus`, `servicerestart`, `capacity`, `license`, `upgradestatus`, `dhcpstatistics`, `ipamstatistics`, `dhcpfailover`, `allrecords`, `zones`, `dtc`, `threatprotection`.

## Collector Scope

The `network`, `range`, and `member` collectors can query all objects in the configured network views. If `-networks` is set, network and range collection is restricted to those CIDRs.

The `ipv4address` collector requires explicit `-ipv4-networks` entries. This avoids accidentally walking very large IPAM spaces without restricting the scope of the other network-based collectors.

Per-address metadata is disabled by default even when IPv4 networks are configured. Enable `-ipv4-address-info` (or `INFOBLOX_IPV4_ADDRESS_INFO=true`) to emit one `infoblox_ipv4address_info` gauge for each address whose WAPI status is `USED`. The `names`, `types`, and `usage` arrays are sorted, deduplicated, and joined with commas. This metric exposes hostname inventory and can create many Prometheus series, so keep `-ipv4-networks` narrowly scoped.

### Migrating IPv4 collector configuration

`-networks` and `INFOBLOX_NETWORKS` no longer configure the `ipv4address` collector. Existing users must copy the CIDRs they want to inspect to `-ipv4-networks` or `INFOBLOX_IPV4_NETWORKS`. Keep the original network option as well when the network, range, DHCP statistics, and IPAM statistics collectors should remain restricted to the same CIDRs.

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
  -networks 10.1.216.0/24 \
  -ipv4-networks 10.1.216.0/24
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
