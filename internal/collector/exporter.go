package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elohmeier/infoblox-exporter/internal/config"
	"github.com/elohmeier/infoblox-exporter/internal/model"
	"github.com/elohmeier/infoblox-exporter/internal/wapi"
	"github.com/prometheus/client_golang/prometheus"
)

type Exporter struct {
	cfg    config.Config
	client *wapi.Client
	logger *slog.Logger

	up                 *prometheus.Desc
	scrapeDuration     *prometheus.Desc
	collectorUp        *prometheus.Desc
	ipv4Configured     *prometheus.Desc
	networkInfo        *prometheus.Desc
	networkUtilization *prometheus.Desc
	networkDHCPUtil    *prometheus.Desc
	networkUtilUpdated *prometheus.Desc
	networkDHCPStatus  *prometheus.Desc
	rangeInfo          *prometheus.Desc
	rangeDHCPUtil      *prometheus.Desc
	rangeDHCPStatus    *prometheus.Desc
	rangeDynamicHosts  *prometheus.Desc
	ipv4StatusCount    *prometheus.Desc
	ipv4TypeCount      *prometheus.Desc
	ipv4UsageCount     *prometheus.Desc
	ipv4LeaseCount     *prometheus.Desc
	ipv4ConflictCount  *prometheus.Desc
	memberInfo         *prometheus.Desc
	memberService      *prometheus.Desc
	restartService     *prometheus.Desc
	serviceRestart     *prometheus.Desc
	serviceRestartReq  *prometheus.Desc
	serviceRestartTime *prometheus.Desc
	capacityInfo       *prometheus.Desc
	capacityUsed       *prometheus.Desc
	capacityMax        *prometheus.Desc
	capacityObjects    *prometheus.Desc
	capacityObjectType *prometheus.Desc
	licenseInfo        *prometheus.Desc
	licenseExpiry      *prometheus.Desc
	upgradeInfo        *prometheus.Desc
	upgradeSteps       *prometheus.Desc
	upgradeStatusTime  *prometheus.Desc
	dhcpStatsUtil      *prometheus.Desc
	dhcpStatsStatus    *prometheus.Desc
	dhcpStatsHosts     *prometheus.Desc
	ipamStatsUtil      *prometheus.Desc
	ipamStatsCount     *prometheus.Desc
	ipamStatsUpdated   *prometheus.Desc
	dhcpFailoverInfo   *prometheus.Desc
	dhcpFailoverValue  *prometheus.Desc
	dnsRecordInfo      *prometheus.Desc
	dnsRecordTTL       *prometheus.Desc
	dnsRecordCount     *prometheus.Desc
	dnsRecordDisabled  *prometheus.Desc
	dnsRecordReclaim   *prometheus.Desc
	dnsZoneInfo        *prometheus.Desc
	dtcObjectInfo      *prometheus.Desc
	dtcObjectCount     *prometheus.Desc
	threatStatValue    *prometheus.Desc
}

func New(cfg config.Config, client *wapi.Client, logger *slog.Logger) *Exporter {
	namespace := "infoblox"
	return &Exporter{
		cfg:    cfg,
		client: client,
		logger: logger,

		up:             prometheus.NewDesc(namespace+"_up", "Whether the last Infoblox scrape completed without collector errors.", nil, nil),
		scrapeDuration: prometheus.NewDesc(namespace+"_scrape_duration_seconds", "Duration of the last Infoblox scrape.", nil, nil),
		collectorUp:    prometheus.NewDesc(namespace+"_collector_up", "Whether the named Infoblox collector completed successfully.", []string{"collector"}, nil),
		ipv4Configured: prometheus.NewDesc(namespace+"_ipv4address_collector_configured", "Whether the IPv4 address collector has explicit networks configured.", nil, nil),

		networkInfo:        prometheus.NewDesc(namespace+"_network_info", "Infoblox network metadata.", []string{"network", "network_view", "comment", "disabled"}, nil),
		networkUtilization: prometheus.NewDesc(namespace+"_network_utilization_ratio", "Infoblox IPAM network utilization ratio.", []string{"network", "network_view"}, nil),
		networkDHCPUtil:    prometheus.NewDesc(namespace+"_network_dhcp_utilization_ratio", "Infoblox network DHCP utilization ratio.", []string{"network", "network_view"}, nil),
		networkUtilUpdated: prometheus.NewDesc(namespace+"_network_utilization_updated_timestamp_seconds", "Timestamp when Infoblox network utilization was last updated.", []string{"network", "network_view"}, nil),
		networkDHCPStatus:  prometheus.NewDesc(namespace+"_network_dhcp_utilization_status", "Infoblox network DHCP utilization status as a one-hot gauge.", []string{"network", "network_view", "status"}, nil),

		rangeInfo:         prometheus.NewDesc(namespace+"_range_info", "Infoblox DHCP range metadata.", []string{"network", "network_view", "start_addr", "end_addr", "name", "comment", "disabled", "server_association_type", "failover_association"}, nil),
		rangeDHCPUtil:     prometheus.NewDesc(namespace+"_range_dhcp_utilization_ratio", "Infoblox DHCP range utilization ratio.", []string{"network", "network_view", "start_addr", "end_addr"}, nil),
		rangeDHCPStatus:   prometheus.NewDesc(namespace+"_range_dhcp_utilization_status", "Infoblox DHCP range utilization status as a one-hot gauge.", []string{"network", "network_view", "start_addr", "end_addr", "status"}, nil),
		rangeDynamicHosts: prometheus.NewDesc(namespace+"_range_dynamic_hosts", "Total DHCP leases issued for the Infoblox range.", []string{"network", "network_view", "start_addr", "end_addr"}, nil),

		ipv4StatusCount:   prometheus.NewDesc(namespace+"_ipv4address_status_count", "Infoblox IPv4 address count by status.", []string{"network", "network_view", "status"}, nil),
		ipv4TypeCount:     prometheus.NewDesc(namespace+"_ipv4address_type_count", "Infoblox IPv4 address count by type.", []string{"network", "network_view", "type"}, nil),
		ipv4UsageCount:    prometheus.NewDesc(namespace+"_ipv4address_usage_count", "Infoblox IPv4 address count by usage.", []string{"network", "network_view", "usage"}, nil),
		ipv4LeaseCount:    prometheus.NewDesc(namespace+"_ipv4address_lease_state_count", "Infoblox IPv4 address count by lease state.", []string{"network", "network_view", "lease_state"}, nil),
		ipv4ConflictCount: prometheus.NewDesc(namespace+"_ipv4address_conflicts", "Infoblox IPv4 addresses with conflict detected.", []string{"network", "network_view"}, nil),

		memberInfo:    prometheus.NewDesc(namespace+"_member_info", "Infoblox Grid member metadata.", []string{"member", "platform", "service_type_configuration"}, nil),
		memberService: prometheus.NewDesc(namespace+"_member_service_status", "Infoblox Grid member service status as a one-hot gauge.", []string{"member", "service", "status"}, nil),

		restartService:     prometheus.NewDesc(namespace+"_restart_service_status", "Infoblox restart service status as a one-hot gauge.", []string{"member", "service", "status"}, nil),
		serviceRestart:     prometheus.NewDesc(namespace+"_service_restart_status_count", "Infoblox service restart status counts.", []string{"parent", "grouped", "state"}, nil),
		serviceRestartReq:  prometheus.NewDesc(namespace+"_service_restart_request_info", "Infoblox service restart request metadata.", []string{"member", "group", "service", "state", "needed", "result", "forced"}, nil),
		serviceRestartTime: prometheus.NewDesc(namespace+"_service_restart_request_updated_timestamp_seconds", "Timestamp when the Infoblox service restart request last changed.", []string{"member", "group", "service"}, nil),

		capacityInfo:       prometheus.NewDesc(namespace+"_capacity_info", "Infoblox member capacity metadata.", []string{"member", "role", "hardware_type"}, nil),
		capacityUsed:       prometheus.NewDesc(namespace+"_capacity_used_ratio", "Infoblox member object capacity usage ratio.", []string{"member", "role", "hardware_type"}, nil),
		capacityMax:        prometheus.NewDesc(namespace+"_capacity_max_objects", "Infoblox member maximum object capacity.", []string{"member", "role", "hardware_type"}, nil),
		capacityObjects:    prometheus.NewDesc(namespace+"_capacity_objects", "Infoblox member total object count.", []string{"member", "role", "hardware_type"}, nil),
		capacityObjectType: prometheus.NewDesc(namespace+"_capacity_object_type_count", "Infoblox member object count by object type.", []string{"member", "object_type"}, nil),

		licenseInfo:   prometheus.NewDesc(namespace+"_license_info", "Infoblox license metadata without license key material.", []string{"scope", "type", "kind", "limit", "limit_context", "expiration_status", "hwid"}, nil),
		licenseExpiry: prometheus.NewDesc(namespace+"_license_expiry_timestamp_seconds", "Infoblox license expiry timestamp.", []string{"scope", "type", "kind", "limit", "limit_context", "hwid"}, nil),

		upgradeInfo:       prometheus.NewDesc(namespace+"_upgrade_status_info", "Infoblox upgrade status metadata.", []string{"type", "member", "upgrade_group", "element_status", "grid_state", "group_state", "ha_status", "status_value", "upgrade_state", "upgrade_test_status", "current_version", "distribution_version", "upload_version", "reverted"}, nil),
		upgradeSteps:      prometheus.NewDesc(namespace+"_upgrade_steps", "Infoblox upgrade step counters.", []string{"type", "member", "upgrade_group", "kind"}, nil),
		upgradeStatusTime: prometheus.NewDesc(namespace+"_upgrade_status_updated_timestamp_seconds", "Timestamp when the Infoblox upgrade status value was updated.", []string{"type", "member", "upgrade_group"}, nil),

		dhcpStatsUtil:   prometheus.NewDesc(namespace+"_dhcp_statistics_utilization_ratio", "Infoblox DHCP statistics utilization ratio.", []string{"object_type", "object"}, nil),
		dhcpStatsStatus: prometheus.NewDesc(namespace+"_dhcp_statistics_utilization_status", "Infoblox DHCP statistics status as a one-hot gauge.", []string{"object_type", "object", "status"}, nil),
		dhcpStatsHosts:  prometheus.NewDesc(namespace+"_dhcp_statistics_hosts", "Infoblox DHCP statistics host counts.", []string{"object_type", "object", "kind"}, nil),

		ipamStatsUtil:    prometheus.NewDesc(namespace+"_ipam_statistics_utilization_ratio", "Infoblox IPAM statistics utilization ratio.", []string{"network", "network_view"}, nil),
		ipamStatsCount:   prometheus.NewDesc(namespace+"_ipam_statistics_count", "Infoblox IPAM statistics counts.", []string{"network", "network_view", "kind"}, nil),
		ipamStatsUpdated: prometheus.NewDesc(namespace+"_ipam_statistics_updated_timestamp_seconds", "Timestamp when Infoblox IPAM utilization was last updated.", []string{"network", "network_view"}, nil),

		dhcpFailoverInfo:  prometheus.NewDesc(namespace+"_dhcp_failover_info", "Infoblox DHCP failover association metadata.", []string{"name", "association_type", "comment", "primary", "secondary", "primary_server_type", "secondary_server_type"}, nil),
		dhcpFailoverValue: prometheus.NewDesc(namespace+"_dhcp_failover_value", "Infoblox DHCP failover numeric settings.", []string{"name", "setting"}, nil),

		dnsRecordInfo:     prometheus.NewDesc(namespace+"_dns_record_info", "Infoblox DNS record metadata. This can be high-cardinality.", []string{"view", "zone", "type", "name", "disabled", "reclaimable"}, nil),
		dnsRecordTTL:      prometheus.NewDesc(namespace+"_dns_record_ttl_seconds", "Infoblox DNS record TTL in seconds. This can be high-cardinality.", []string{"view", "zone", "type", "name"}, nil),
		dnsRecordCount:    prometheus.NewDesc(namespace+"_dns_record_count", "Infoblox DNS record count by view, zone, and type.", []string{"view", "zone", "type"}, nil),
		dnsRecordDisabled: prometheus.NewDesc(namespace+"_dns_record_disabled_count", "Infoblox disabled DNS record count by view, zone, and type.", []string{"view", "zone", "type"}, nil),
		dnsRecordReclaim:  prometheus.NewDesc(namespace+"_dns_record_reclaimable_count", "Infoblox reclaimable DNS record count by view, zone, and type.", []string{"view", "zone", "type"}, nil),
		dnsZoneInfo:       prometheus.NewDesc(namespace+"_dns_zone_info", "Infoblox DNS zone metadata.", []string{"view", "zone", "type", "comment", "disabled"}, nil),

		dtcObjectInfo:  prometheus.NewDesc(namespace+"_dtc_object_info", "Infoblox DTC object metadata.", []string{"name", "abstract_type", "display_type", "status", "comment"}, nil),
		dtcObjectCount: prometheus.NewDesc(namespace+"_dtc_object_count", "Infoblox DTC object count by type and status.", []string{"abstract_type", "display_type", "status"}, nil),

		threatStatValue: prometheus.NewDesc(namespace+"_threatprotection_stat_value", "Infoblox threat protection numeric statistic values.", []string{"member", "stat"}, nil),
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		e.up, e.scrapeDuration, e.collectorUp, e.ipv4Configured,
		e.networkInfo, e.networkUtilization, e.networkDHCPUtil, e.networkUtilUpdated, e.networkDHCPStatus,
		e.rangeInfo, e.rangeDHCPUtil, e.rangeDHCPStatus, e.rangeDynamicHosts,
		e.ipv4StatusCount, e.ipv4TypeCount, e.ipv4UsageCount, e.ipv4LeaseCount, e.ipv4ConflictCount,
		e.memberInfo, e.memberService,
		e.restartService, e.serviceRestart, e.serviceRestartReq, e.serviceRestartTime,
		e.capacityInfo, e.capacityUsed, e.capacityMax, e.capacityObjects, e.capacityObjectType,
		e.licenseInfo, e.licenseExpiry,
		e.upgradeInfo, e.upgradeSteps, e.upgradeStatusTime,
		e.dhcpStatsUtil, e.dhcpStatsStatus, e.dhcpStatsHosts,
		e.ipamStatsUtil, e.ipamStatsCount, e.ipamStatsUpdated,
		e.dhcpFailoverInfo, e.dhcpFailoverValue,
		e.dnsRecordInfo, e.dnsRecordTTL, e.dnsRecordCount, e.dnsRecordDisabled, e.dnsRecordReclaim, e.dnsZoneInfo,
		e.dtcObjectInfo, e.dtcObjectCount,
		e.threatStatValue,
	} {
		ch <- desc
	}
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.Timeout)
	defer cancel()

	errors := 0
	if e.enabled("network") {
		errors += e.runCollector(ctx, ch, "network", e.collectNetworks)
	}
	if e.enabled("range") {
		errors += e.runCollector(ctx, ch, "range", e.collectRanges)
	}
	if e.enabled("ipv4address") {
		configured := 0.0
		if len(e.cfg.Networks) > 0 {
			configured = 1
			errors += e.runCollector(ctx, ch, "ipv4address", e.collectIPv4Addresses)
		} else {
			ch <- prometheus.MustNewConstMetric(e.collectorUp, prometheus.GaugeValue, 1, "ipv4address")
		}
		ch <- prometheus.MustNewConstMetric(e.ipv4Configured, prometheus.GaugeValue, configured)
	}
	if e.enabled("member") {
		errors += e.runCollector(ctx, ch, "member", e.collectMembers)
	}
	if e.enabled("restartservicestatus") {
		errors += e.runCollector(ctx, ch, "restartservicestatus", e.collectRestartServiceStatus)
	}
	if e.enabled("servicerestart") {
		errors += e.runCollector(ctx, ch, "servicerestart", e.collectServiceRestart)
	}
	if e.enabled("capacity") {
		errors += e.runCollector(ctx, ch, "capacity", e.collectCapacity)
	}
	if e.enabled("license") {
		errors += e.runCollector(ctx, ch, "license", e.collectLicenses)
	}
	if e.enabled("upgradestatus") {
		errors += e.runCollector(ctx, ch, "upgradestatus", e.collectUpgradeStatus)
	}
	if e.enabled("dhcpstatistics") {
		errors += e.runCollector(ctx, ch, "dhcpstatistics", e.collectDHCPStatistics)
	}
	if e.enabled("ipamstatistics") {
		errors += e.runCollector(ctx, ch, "ipamstatistics", e.collectIPAMStatistics)
	}
	if e.enabled("dhcpfailover") {
		errors += e.runCollector(ctx, ch, "dhcpfailover", e.collectDHCPFailover)
	}
	if e.enabled("allrecords") {
		errors += e.runCollector(ctx, ch, "allrecords", e.collectAllRecords)
	}
	if e.enabled("zones") {
		errors += e.runCollector(ctx, ch, "zones", e.collectZones)
	}
	if e.enabled("dtc") {
		errors += e.runCollector(ctx, ch, "dtc", e.collectDTC)
	}
	if e.enabled("threatprotection") {
		errors += e.runCollector(ctx, ch, "threatprotection", e.collectThreatProtection)
	}

	up := 1.0
	if errors > 0 {
		up = 0
	}
	ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, up)
	ch <- prometheus.MustNewConstMetric(e.scrapeDuration, prometheus.GaugeValue, time.Since(start).Seconds())
}

func (e *Exporter) enabled(name string) bool {
	return !e.cfg.IsModuleDisabled(name)
}

func (e *Exporter) runCollector(_ context.Context, ch chan<- prometheus.Metric, name string, fn func(context.Context, chan<- prometheus.Metric) error) int {
	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.Timeout)
	defer cancel()

	if err := fn(ctx, ch); err != nil {
		e.logger.Warn("collector failed", "collector", name, "err", err)
		ch <- prometheus.MustNewConstMetric(e.collectorUp, prometheus.GaugeValue, 0, name)
		return 1
	}
	ch <- prometheus.MustNewConstMetric(e.collectorUp, prometheus.GaugeValue, 1, name)
	return 0
}

func (e *Exporter) collectNetworks(ctx context.Context, ch chan<- prometheus.Metric) error {
	views := viewsOrSingleEmpty(e.cfg.NetworkViews)
	for _, view := range views {
		if len(e.cfg.Networks) > 0 {
			for _, network := range e.cfg.Networks {
				if err := e.collectNetworksForQuery(ctx, ch, view, network); err != nil {
					return err
				}
			}
			continue
		}
		if err := e.collectNetworksForQuery(ctx, ch, view, ""); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exporter) collectNetworksForQuery(ctx context.Context, ch chan<- prometheus.Metric, view string, network string) error {
	params := fields("network", "network_view", "comment", "disable", "utilization", "utilization_update", "dhcp_utilization", "dhcp_utilization_status")
	if view != "" {
		params.Set("network_view", view)
	}
	if network != "" {
		params.Set("network", network)
	}

	networks, err := wapi.FetchAll[model.Network](ctx, e.client, "network", params)
	if err != nil {
		return err
	}
	for _, item := range networks {
		networkView := valueOr(item.NetworkView, view, "default")
		ch <- prometheus.MustNewConstMetric(e.networkInfo, prometheus.GaugeValue, 1, item.Network, networkView, item.Comment, boolLabel(item.Disable))
		if item.Utilization.Valid {
			ch <- prometheus.MustNewConstMetric(e.networkUtilization, prometheus.GaugeValue, utilizationRatio(item.Utilization.Value), item.Network, networkView)
		}
		if item.DHCPUtilization.Valid {
			ch <- prometheus.MustNewConstMetric(e.networkDHCPUtil, prometheus.GaugeValue, utilizationRatio(item.DHCPUtilization.Value), item.Network, networkView)
		}
		if item.UtilizationUpdate.Valid {
			ch <- prometheus.MustNewConstMetric(e.networkUtilUpdated, prometheus.GaugeValue, float64(item.UtilizationUpdate.Value), item.Network, networkView)
		}
		if item.DHCPUtilizationStatus != "" {
			ch <- prometheus.MustNewConstMetric(e.networkDHCPStatus, prometheus.GaugeValue, 1, item.Network, networkView, item.DHCPUtilizationStatus)
		}
	}
	return nil
}

func (e *Exporter) collectRanges(ctx context.Context, ch chan<- prometheus.Metric) error {
	views := viewsOrSingleEmpty(e.cfg.NetworkViews)
	for _, view := range views {
		if len(e.cfg.Networks) > 0 {
			for _, network := range e.cfg.Networks {
				if err := e.collectRangesForQuery(ctx, ch, view, network); err != nil {
					return err
				}
			}
			continue
		}
		if err := e.collectRangesForQuery(ctx, ch, view, ""); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exporter) collectRangesForQuery(ctx context.Context, ch chan<- prometheus.Metric, view string, network string) error {
	params := fields("network", "network_view", "start_addr", "end_addr", "name", "comment", "disable", "server_association_type", "failover_association", "dhcp_utilization", "dhcp_utilization_status", "dynamic_hosts")
	if view != "" {
		params.Set("network_view", view)
	}
	if network != "" {
		params.Set("network", network)
	}

	ranges, err := wapi.FetchAll[model.Range](ctx, e.client, "range", params)
	if err != nil {
		return err
	}
	for _, item := range ranges {
		networkView := valueOr(item.NetworkView, view, "default")
		ch <- prometheus.MustNewConstMetric(
			e.rangeInfo,
			prometheus.GaugeValue,
			1,
			item.Network,
			networkView,
			item.StartAddr,
			item.EndAddr,
			item.Name,
			item.Comment,
			boolLabel(item.Disable),
			item.ServerAssociationType,
			item.FailoverAssociation,
		)
		if item.DHCPUtilization.Valid {
			ch <- prometheus.MustNewConstMetric(e.rangeDHCPUtil, prometheus.GaugeValue, utilizationRatio(item.DHCPUtilization.Value), item.Network, networkView, item.StartAddr, item.EndAddr)
		}
		if item.DHCPUtilizationStatus != "" {
			ch <- prometheus.MustNewConstMetric(e.rangeDHCPStatus, prometheus.GaugeValue, 1, item.Network, networkView, item.StartAddr, item.EndAddr, item.DHCPUtilizationStatus)
		}
		if item.DynamicHosts.Valid {
			ch <- prometheus.MustNewConstMetric(e.rangeDynamicHosts, prometheus.GaugeValue, float64(item.DynamicHosts.Value), item.Network, networkView, item.StartAddr, item.EndAddr)
		}
	}
	return nil
}

func (e *Exporter) collectIPv4Addresses(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, view := range viewsOrSingleEmpty(e.cfg.NetworkViews) {
		for _, network := range e.cfg.Networks {
			if err := e.collectIPv4AddressesForNetwork(ctx, ch, view, network); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Exporter) collectIPv4AddressesForNetwork(ctx context.Context, ch chan<- prometheus.Metric, view string, network string) error {
	params := fields("ip_address", "network", "network_view", "status", "lease_state", "types", "usage", "is_conflict")
	params.Set("network", network)
	if view != "" {
		params.Set("network_view", view)
	}

	addresses, err := wapi.FetchAll[model.IPv4Address](ctx, e.client, "ipv4address", params)
	if err != nil {
		return err
	}

	statusCounts := map[string]int{}
	typeCounts := map[string]int{}
	usageCounts := map[string]int{}
	leaseCounts := map[string]int{}
	conflicts := 0
	networkView := valueOr(view, "default")

	for _, address := range addresses {
		if address.NetworkView != "" {
			networkView = address.NetworkView
		}
		statusCounts[valueOr(address.Status, "unknown")]++
		leaseCounts[valueOr(address.LeaseState, "none")]++
		if len(address.Types) == 0 {
			typeCounts["none"]++
		}
		for _, typ := range address.Types {
			typeCounts[valueOr(typ, "unknown")]++
		}
		if len(address.Usage) == 0 {
			usageCounts["none"]++
		}
		for _, usage := range address.Usage {
			usageCounts[valueOr(usage, "unknown")]++
		}
		if address.IsConflict {
			conflicts++
		}
	}

	emitCounts(ch, e.ipv4StatusCount, statusCounts, network, networkView)
	emitCounts(ch, e.ipv4TypeCount, typeCounts, network, networkView)
	emitCounts(ch, e.ipv4UsageCount, usageCounts, network, networkView)
	emitCounts(ch, e.ipv4LeaseCount, leaseCounts, network, networkView)
	ch <- prometheus.MustNewConstMetric(e.ipv4ConflictCount, prometheus.GaugeValue, float64(conflicts), network, networkView)
	return nil
}

func (e *Exporter) collectMembers(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("host_name", "platform", "service_type_configuration", "service_status")
	members, err := wapi.FetchAll[model.Member](ctx, e.client, "member", params)
	if err != nil {
		return err
	}
	for _, member := range members {
		ch <- prometheus.MustNewConstMetric(e.memberInfo, prometheus.GaugeValue, 1, member.HostName, member.Platform, member.ServiceTypeConfiguration)
		for _, rawStatus := range member.ServiceStatus {
			service, status := memberServiceStatus(rawStatus)
			if service == "" || status == "" {
				continue
			}
			ch <- prometheus.MustNewConstMetric(e.memberService, prometheus.GaugeValue, 1, member.HostName, service, status)
		}
	}
	return nil
}

func (e *Exporter) collectRestartServiceStatus(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("member", "dhcp_status", "dns_status", "reporting_status")
	statuses, err := wapi.FetchAll[model.RestartServiceStatus](ctx, e.client, "restartservicestatus", params)
	if err != nil {
		return err
	}
	for _, status := range statuses {
		emitStatus(ch, e.restartService, status.Member, "dhcp", status.DHCPStatus)
		emitStatus(ch, e.restartService, status.Member, "dns", status.DNSStatus)
		emitStatus(ch, e.restartService, status.Member, "reporting", status.ReportingStatus)
	}
	return nil
}

func (e *Exporter) collectServiceRestart(ctx context.Context, ch chan<- prometheus.Metric) error {
	statusParams := fields("parent", "grouped", "failures", "finished", "needed_restart", "no_restart", "pending", "pending_restart", "processing", "restarting", "success", "timeouts")
	statuses, err := wapi.FetchAll[model.ServiceRestartStatus](ctx, e.client, "grid:servicerestart:status", statusParams)
	if err != nil {
		return err
	}
	for _, status := range statuses {
		parent := valueOr(status.Parent, status.Ref, "grid")
		grouped := valueOr(status.Grouped, "unknown")
		emitUint(ch, e.serviceRestart, status.Failures, parent, grouped, "failures")
		emitUint(ch, e.serviceRestart, status.Finished, parent, grouped, "finished")
		emitUint(ch, e.serviceRestart, status.NeededRestart, parent, grouped, "needed_restart")
		emitUint(ch, e.serviceRestart, status.NoRestart, parent, grouped, "no_restart")
		emitUint(ch, e.serviceRestart, status.Pending, parent, grouped, "pending")
		emitUint(ch, e.serviceRestart, status.PendingRestart, parent, grouped, "pending_restart")
		emitUint(ch, e.serviceRestart, status.Processing, parent, grouped, "processing")
		emitUint(ch, e.serviceRestart, status.Restarting, parent, grouped, "restarting")
		emitUint(ch, e.serviceRestart, status.Success, parent, grouped, "success")
		emitUint(ch, e.serviceRestart, status.Timeouts, parent, grouped, "timeouts")
	}

	reqParams := fields("member", "group", "service", "state", "needed", "result", "forced", "last_updated_time", "error")
	requests, err := wapi.FetchAll[model.ServiceRestartRequest](ctx, e.client, "grid:servicerestart:request", reqParams)
	if err != nil {
		return err
	}
	for _, request := range requests {
		ch <- prometheus.MustNewConstMetric(
			e.serviceRestartReq,
			prometheus.GaugeValue,
			1,
			request.Member,
			request.Group,
			request.Service,
			request.State,
			request.Needed,
			request.Result,
			boolLabel(request.Forced),
		)
		if request.LastUpdatedTime.Valid {
			ch <- prometheus.MustNewConstMetric(e.serviceRestartTime, prometheus.GaugeValue, float64(request.LastUpdatedTime.Value), request.Member, request.Group, request.Service)
		}
	}
	return nil
}

func (e *Exporter) collectCapacity(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("name", "hardware_type", "role", "max_capacity", "percent_used", "total_objects", "object_counts")
	members, err := wapi.FetchAll[model.Member](ctx, e.client, "member", fields("host_name"))
	if err != nil {
		return err
	}
	memberNames := make([]string, 0, len(members))
	for _, member := range members {
		if member.HostName != "" {
			memberNames = append(memberNames, member.HostName)
		}
	}
	if len(memberNames) == 0 {
		memberNames = append(memberNames, e.client.Hostname())
	}
	for _, memberName := range memberNames {
		if memberName == "" {
			continue
		}
		query := cloneValues(params)
		query.Set("name", memberName)
		reports, err := wapi.FetchAll[model.CapacityReport](ctx, e.client, "capacityreport", query)
		if err != nil {
			return err
		}
		for _, report := range reports {
			ch <- prometheus.MustNewConstMetric(e.capacityInfo, prometheus.GaugeValue, 1, report.Name, report.Role, report.HardwareType)
			if report.PercentUsed.Valid {
				ch <- prometheus.MustNewConstMetric(e.capacityUsed, prometheus.GaugeValue, utilizationRatio(report.PercentUsed.Value), report.Name, report.Role, report.HardwareType)
			}
			emitUint(ch, e.capacityMax, report.MaxCapacity, report.Name, report.Role, report.HardwareType)
			emitUint(ch, e.capacityObjects, report.TotalObjects, report.Name, report.Role, report.HardwareType)
			for _, count := range report.ObjectCounts {
				objectType := firstString(count, "type", "type_name", "object_type", "name")
				value, ok := firstNumber(count, "count", "total", "value")
				if objectType == "" || !ok {
					continue
				}
				ch <- prometheus.MustNewConstMetric(e.capacityObjectType, prometheus.GaugeValue, value, report.Name, objectType)
			}
		}
	}
	return nil
}

func (e *Exporter) collectLicenses(ctx context.Context, ch chan<- prometheus.Metric) error {
	memberParams := fields("type", "kind", "limit", "limit_context", "expiration_status", "expiry_date")
	memberLicenses, err := wapi.FetchAll[model.License](ctx, e.client, "member:license", memberParams)
	if err != nil {
		return err
	}
	for _, license := range memberLicenses {
		license.Scope = "member"
		e.emitLicense(ch, license)
	}

	gridParams := fields("type", "limit", "limit_context", "expiration_status", "expiry_date")
	gridLicenses, err := wapi.FetchAll[model.License](ctx, e.client, "license:gridwide", gridParams)
	if err != nil {
		return err
	}
	for _, license := range gridLicenses {
		license.Scope = "gridwide"
		license.Kind = valueOr(license.Kind, "Gridwide")
		e.emitLicense(ch, license)
	}
	return nil
}

func (e *Exporter) emitLicense(ch chan<- prometheus.Metric, license model.License) {
	ch <- prometheus.MustNewConstMetric(
		e.licenseInfo,
		prometheus.GaugeValue,
		1,
		license.Scope,
		license.Type,
		license.Kind,
		license.Limit,
		license.LimitContext,
		license.ExpirationStatus,
		license.HWID,
	)
	if license.ExpiryDate.Valid {
		ch <- prometheus.MustNewConstMetric(e.licenseExpiry, prometheus.GaugeValue, float64(license.ExpiryDate.Value), license.Scope, license.Type, license.Kind, license.Limit, license.LimitContext, license.HWID)
	}
}

func (e *Exporter) collectUpgradeStatus(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("type", "member", "upgrade_group", "current_version", "distribution_version", "upload_version", "element_status", "grid_state", "group_state", "ha_status", "status_value", "status_value_update_time", "steps_completed", "steps_total", "upgrade_state", "upgrade_test_status", "reverted")
	for _, typ := range e.cfg.UpgradeStatusTypes {
		query := cloneValues(params)
		query.Set("type", typ)
		statuses, err := wapi.FetchAll[model.UpgradeStatus](ctx, e.client, "upgradestatus", query)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			statusType := valueOr(status.Type, typ)
			ch <- prometheus.MustNewConstMetric(
				e.upgradeInfo,
				prometheus.GaugeValue,
				1,
				statusType,
				status.Member,
				status.UpgradeGroup,
				status.ElementStatus,
				status.GridState,
				status.GroupState,
				status.HAStatus,
				status.StatusValue,
				status.UpgradeState,
				status.UpgradeTestStatus,
				status.CurrentVersion,
				status.DistributionVersion,
				status.UploadVersion,
				boolLabel(status.Reverted),
			)
			emitUint(ch, e.upgradeSteps, status.StepsCompleted, statusType, status.Member, status.UpgradeGroup, "completed")
			emitUint(ch, e.upgradeSteps, status.StepsTotal, statusType, status.Member, status.UpgradeGroup, "total")
			if status.StatusValueUpdateTime.Valid {
				ch <- prometheus.MustNewConstMetric(e.upgradeStatusTime, prometheus.GaugeValue, float64(status.StatusValueUpdateTime.Value), statusType, status.Member, status.UpgradeGroup)
			}
		}
	}
	return nil
}

func (e *Exporter) collectDHCPStatistics(ctx context.Context, ch chan<- prometheus.Metric) error {
	objects, err := e.dhcpStatisticsObjects(ctx)
	if err != nil {
		return err
	}
	params := fields("dhcp_utilization", "dhcp_utilization_status", "dynamic_hosts", "static_hosts", "total_hosts")
	for _, object := range objects {
		query := cloneValues(params)
		query.Set("statistics_object", object.ref)
		stats, err := wapi.FetchAllUnpaged[model.DHCPStatistics](ctx, e.client, "dhcp:statistics", query)
		if err != nil {
			return err
		}
		for _, stat := range stats {
			if stat.DHCPUtilization.Valid {
				ch <- prometheus.MustNewConstMetric(e.dhcpStatsUtil, prometheus.GaugeValue, utilizationRatio(stat.DHCPUtilization.Value), object.kind, object.name)
			}
			if stat.DHCPUtilizationStatus != "" {
				ch <- prometheus.MustNewConstMetric(e.dhcpStatsStatus, prometheus.GaugeValue, 1, object.kind, object.name, stat.DHCPUtilizationStatus)
			}
			emitUint(ch, e.dhcpStatsHosts, stat.DynamicHosts, object.kind, object.name, "dynamic")
			emitUint(ch, e.dhcpStatsHosts, stat.StaticHosts, object.kind, object.name, "static")
			emitUint(ch, e.dhcpStatsHosts, stat.TotalHosts, object.kind, object.name, "total")
		}
	}
	return nil
}

type statisticsObject struct {
	kind string
	name string
	ref  string
}

func (e *Exporter) dhcpStatisticsObjects(ctx context.Context) ([]statisticsObject, error) {
	var objects []statisticsObject
	for _, view := range viewsOrSingleEmpty(e.cfg.NetworkViews) {
		if len(e.cfg.Networks) > 0 {
			for _, network := range e.cfg.Networks {
				networks, err := e.fetchNetworkRefs(ctx, view, network)
				if err != nil {
					return nil, err
				}
				objects = append(objects, networkStatisticsObjects(networks)...)
				ranges, err := e.fetchRangeRefs(ctx, view, network)
				if err != nil {
					return nil, err
				}
				objects = append(objects, rangeStatisticsObjects(ranges)...)
			}
			continue
		}
		networks, err := e.fetchNetworkRefs(ctx, view, "")
		if err != nil {
			return nil, err
		}
		objects = append(objects, networkStatisticsObjects(networks)...)
		ranges, err := e.fetchRangeRefs(ctx, view, "")
		if err != nil {
			return nil, err
		}
		objects = append(objects, rangeStatisticsObjects(ranges)...)
	}
	return objects, nil
}

func (e *Exporter) fetchNetworkRefs(ctx context.Context, view string, network string) ([]model.Network, error) {
	params := fields("network", "network_view")
	if view != "" {
		params.Set("network_view", view)
	}
	if network != "" {
		params.Set("network", network)
	}
	return wapi.FetchAll[model.Network](ctx, e.client, "network", params)
}

func (e *Exporter) fetchRangeRefs(ctx context.Context, view string, network string) ([]model.Range, error) {
	params := fields("network", "network_view", "start_addr", "end_addr")
	if view != "" {
		params.Set("network_view", view)
	}
	if network != "" {
		params.Set("network", network)
	}
	return wapi.FetchAll[model.Range](ctx, e.client, "range", params)
}

func networkStatisticsObjects(networks []model.Network) []statisticsObject {
	objects := make([]statisticsObject, 0, len(networks))
	for _, network := range networks {
		if network.Ref == "" {
			continue
		}
		objects = append(objects, statisticsObject{kind: "network", name: network.Network, ref: network.Ref})
	}
	return objects
}

func rangeStatisticsObjects(ranges []model.Range) []statisticsObject {
	objects := make([]statisticsObject, 0, len(ranges))
	for _, item := range ranges {
		if item.Ref == "" {
			continue
		}
		name := item.Network
		if item.StartAddr != "" || item.EndAddr != "" {
			name = item.StartAddr + "-" + item.EndAddr
		}
		objects = append(objects, statisticsObject{kind: "range", name: name, ref: item.Ref})
	}
	return objects
}

func (e *Exporter) collectIPAMStatistics(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, view := range viewsOrSingleEmpty(e.cfg.NetworkViews) {
		if len(e.cfg.Networks) > 0 {
			for _, network := range e.cfg.Networks {
				if err := e.collectIPAMStatisticsForQuery(ctx, ch, view, network); err != nil {
					return err
				}
			}
			continue
		}
		if err := e.collectIPAMStatisticsForQuery(ctx, ch, view, ""); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exporter) collectIPAMStatisticsForQuery(ctx context.Context, ch chan<- prometheus.Metric, view string, network string) error {
	params := fields("network", "network_view", "cidr", "unmanaged_count", "utilization", "utilization_update")
	if view != "" {
		params.Set("network_view", view)
	}
	if network != "" {
		params.Set("network", network)
	}
	stats, err := wapi.FetchAll[model.IPAMStatistics](ctx, e.client, "ipam:statistics", params)
	if err != nil {
		return err
	}
	for _, stat := range stats {
		networkView := valueOr(stat.NetworkView, view, "default")
		if stat.Utilization.Valid {
			ch <- prometheus.MustNewConstMetric(e.ipamStatsUtil, prometheus.GaugeValue, utilizationRatio(stat.Utilization.Value), stat.Network, networkView)
		}
		emitUint(ch, e.ipamStatsCount, stat.ConflictCount, stat.Network, networkView, "conflict")
		emitUint(ch, e.ipamStatsCount, stat.UnmanagedCount, stat.Network, networkView, "unmanaged")
		if stat.UtilizationUpdate.Valid {
			ch <- prometheus.MustNewConstMetric(e.ipamStatsUpdated, prometheus.GaugeValue, float64(stat.UtilizationUpdate.Value), stat.Network, networkView)
		}
	}
	return nil
}

func (e *Exporter) collectDHCPFailover(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("name", "association_type", "comment", "primary", "secondary", "primary_server_type", "secondary_server_type", "load_balance_split", "max_client_lead_time", "max_response_delay", "max_unacked_updates")
	failovers, err := wapi.FetchAll[model.DHCPFailover](ctx, e.client, "dhcpfailover", params)
	if err != nil {
		return err
	}
	for _, failover := range failovers {
		ch <- prometheus.MustNewConstMetric(
			e.dhcpFailoverInfo,
			prometheus.GaugeValue,
			1,
			failover.Name,
			failover.AssociationType,
			failover.Comment,
			failover.Primary,
			failover.Secondary,
			failover.PrimaryServerType,
			failover.SecondaryServerType,
		)
		emitUint(ch, e.dhcpFailoverValue, failover.LoadBalanceSplit, failover.Name, "load_balance_split")
		emitUint(ch, e.dhcpFailoverValue, failover.MaxClientLeadTime, failover.Name, "max_client_lead_time")
		emitUint(ch, e.dhcpFailoverValue, failover.MaxResponseDelay, failover.Name, "max_response_delay")
		emitUint(ch, e.dhcpFailoverValue, failover.MaxUnackedUpdates, failover.Name, "max_unacked_updates")
	}
	return nil
}

func (e *Exporter) collectAllRecords(ctx context.Context, ch chan<- prometheus.Metric) error {
	if len(e.cfg.Zones) == 0 {
		return nil
	}
	for _, view := range viewsOrSingleEmpty(e.cfg.DNSViews) {
		for _, zone := range e.cfg.Zones {
			if err := e.collectAllRecordsForQuery(ctx, ch, view, zone); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Exporter) collectAllRecordsForQuery(ctx context.Context, ch chan<- prometheus.Metric, view string, zone string) error {
	params := fields("name", "type", "view", "zone", "disable", "reclaimable", "ttl")
	if view != "" {
		params.Set("view", view)
	}
	if zone != "" {
		params.Set("zone", zone)
	}
	records, err := wapi.FetchAll[model.AllRecord](ctx, e.client, "allrecords", params)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	disabled := map[string]int{}
	reclaimable := map[string]int{}
	for _, record := range records {
		recordView := valueOr(record.View, view, "default")
		recordZone := valueOr(record.Zone, zone, "unknown")
		recordType := valueOr(record.Type, "unknown")
		key := recordKey(recordView, recordZone, recordType)
		counts[key]++
		if record.Disable {
			disabled[key]++
		}
		if record.Reclaimable {
			reclaimable[key]++
		}
		ch <- prometheus.MustNewConstMetric(e.dnsRecordInfo, prometheus.GaugeValue, 1, recordView, recordZone, recordType, record.Name, boolLabel(record.Disable), boolLabel(record.Reclaimable))
		if record.TTL.Valid {
			ch <- prometheus.MustNewConstMetric(e.dnsRecordTTL, prometheus.GaugeValue, float64(record.TTL.Value), recordView, recordZone, recordType, record.Name)
		}
	}
	emitRecordCounts(ch, e.dnsRecordCount, counts)
	emitRecordCounts(ch, e.dnsRecordDisabled, disabled)
	emitRecordCounts(ch, e.dnsRecordReclaim, reclaimable)
	return nil
}

func (e *Exporter) collectZones(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, objectType := range []string{"zone_auth", "zone_forward", "zone_stub", "zone_delegated", "zone_rp"} {
		if err := e.collectZonesForObject(ctx, ch, objectType); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exporter) collectZonesForObject(ctx context.Context, ch chan<- prometheus.Metric, objectType string) error {
	for _, view := range viewsOrSingleEmpty(e.cfg.DNSViews) {
		params := fields("fqdn", "view", "comment", "disable")
		if view != "" {
			params.Set("view", view)
		}
		zones, err := wapi.FetchAll[model.Zone](ctx, e.client, objectType, params)
		if err != nil {
			return err
		}
		for _, zone := range zones {
			if len(e.cfg.Zones) > 0 && !contains(e.cfg.Zones, zone.FQDN) {
				continue
			}
			ch <- prometheus.MustNewConstMetric(e.dnsZoneInfo, prometheus.GaugeValue, 1, valueOr(zone.View, view, "default"), zone.FQDN, objectType, zone.Comment, boolLabel(zone.Disable))
		}
	}
	return nil
}

func (e *Exporter) collectDTC(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("name", "comment", "abstract_type", "display_type", "status")
	objects, err := wapi.FetchAll[model.DTCObject](ctx, e.client, "dtc:object", params)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, object := range objects {
		ch <- prometheus.MustNewConstMetric(e.dtcObjectInfo, prometheus.GaugeValue, 1, object.Name, object.AbstractType, object.DisplayType, object.Status, object.Comment)
		key := recordKey(valueOr(object.AbstractType, "unknown"), valueOr(object.DisplayType, "unknown"), valueOr(object.Status, "unknown"))
		counts[key]++
	}
	emitRecordCounts(ch, e.dtcObjectCount, counts)
	return nil
}

func (e *Exporter) collectThreatProtection(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("member", "stat_infos")
	stats, err := wapi.FetchAll[model.ThreatProtectionStatistics](ctx, e.client, "threatprotection:statistics", params)
	if err != nil {
		return err
	}
	for _, stat := range stats {
		member := valueOr(stat.Member, "grid")
		values := map[string]float64{}
		for _, info := range stat.StatInfos {
			collectNumericLeaves("", info, values)
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			ch <- prometheus.MustNewConstMetric(e.threatStatValue, prometheus.GaugeValue, values[key], member, key)
		}
	}
	return nil
}

func fields(names ...string) url.Values {
	values := url.Values{}
	values.Set("_return_fields", strings.Join(names, ","))
	return values
}

func viewsOrSingleEmpty(views []string) []string {
	if len(views) == 0 {
		return []string{""}
	}
	return views
}

func valueOr(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func utilizationRatio(value uint64) float64 {
	if value <= 100 {
		return float64(value) / 100
	}
	return float64(value) / 100000
}

func emitCounts(ch chan<- prometheus.Metric, desc *prometheus.Desc, counts map[string]int, network string, networkView string) {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(counts[key]), network, networkView, key)
	}
}

func emitStatus(ch chan<- prometheus.Metric, desc *prometheus.Desc, labels ...string) {
	for _, label := range labels {
		if label == "" {
			return
		}
	}
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 1, labels...)
}

func emitUint(ch chan<- prometheus.Metric, desc *prometheus.Desc, value model.Uint64, labels ...string) {
	if !value.Valid {
		return
	}
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(value.Value), labels...)
}

func emitRecordCounts(ch chan<- prometheus.Metric, desc *prometheus.Desc, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 3)
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(counts[key]), parts[0], parts[1], parts[2])
	}
}

func recordKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func firstNumber(raw map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if number, ok := numberFromAny(value); ok {
			return number, true
		}
	}
	return 0, false
}

func numberFromAny(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case jsonNumber:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}

func collectNumericLeaves(prefix string, value interface{}, out map[string]float64) {
	if number, ok := numberFromAny(value); ok {
		key := valueOr(prefix, "value")
		out[key] += number
		return
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			nextPrefix := key
			if prefix != "" {
				nextPrefix = prefix + "." + key
			}
			collectNumericLeaves(nextPrefix, typed[key], out)
		}
	case []interface{}:
		for _, item := range typed {
			collectNumericLeaves(prefix, item, out)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}

func memberServiceStatus(raw map[string]interface{}) (string, string) {
	service := firstString(raw, "service", "name", "service_name", "service_type", "type")
	status := firstString(raw, "status", "state", "service_status")
	if status == "" {
		if enabled, ok := raw["enabled"].(bool); ok {
			status = fmt.Sprintf("enabled_%t", enabled)
		}
	}
	return service, status
}

func firstString(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed
			}
		case fmt.Stringer:
			if typed.String() != "" {
				return typed.String()
			}
		}
	}
	return ""
}
