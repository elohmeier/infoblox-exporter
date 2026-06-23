package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elohmeier/infoblox-exporter/internal/config"
	"github.com/elohmeier/infoblox-exporter/internal/model"
	"github.com/elohmeier/infoblox-exporter/internal/wapi"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type Exporter struct {
	cfg    config.Config
	client *wapi.Client
	logger *slog.Logger

	refreshMu       sync.Mutex
	cacheMu         sync.Mutex
	startOnce       sync.Once
	schedulerCancel context.CancelFunc
	moduleStates    map[string]*moduleState

	up                 *prometheus.GaugeVec
	scrapeDuration     *prometheus.GaugeVec
	collectorUp        *prometheus.GaugeVec
	ipv4Configured     *prometheus.GaugeVec
	refreshDuration    *prometheus.GaugeVec
	refreshTotal       *prometheus.CounterVec
	refreshErrorsTotal *prometheus.CounterVec
	cacheLastAttempt   *prometheus.GaugeVec
	cacheLastSuccess   *prometheus.GaugeVec
	cacheAge           *prometheus.GaugeVec
	cacheStale         *prometheus.GaugeVec
	networkInfo        *prometheus.GaugeVec
	networkUtilization *prometheus.GaugeVec
	networkDHCPUtil    *prometheus.GaugeVec
	networkUtilUpdated *prometheus.GaugeVec
	networkDHCPStatus  *prometheus.GaugeVec
	rangeInfo          *prometheus.GaugeVec
	rangeDHCPUtil      *prometheus.GaugeVec
	rangeDHCPStatus    *prometheus.GaugeVec
	rangeDynamicHosts  *prometheus.GaugeVec
	ipv4StatusCount    *prometheus.GaugeVec
	ipv4TypeCount      *prometheus.GaugeVec
	ipv4UsageCount     *prometheus.GaugeVec
	ipv4LeaseCount     *prometheus.GaugeVec
	ipv4ConflictCount  *prometheus.GaugeVec
	memberInfo         *prometheus.GaugeVec
	memberService      *prometheus.GaugeVec
	restartService     *prometheus.GaugeVec
	serviceRestart     *prometheus.GaugeVec
	serviceRestartReq  *prometheus.GaugeVec
	serviceRestartTime *prometheus.GaugeVec
	capacityInfo       *prometheus.GaugeVec
	capacityUsed       *prometheus.GaugeVec
	capacityMax        *prometheus.GaugeVec
	capacityObjects    *prometheus.GaugeVec
	capacityObjectType *prometheus.GaugeVec
	licenseInfo        *prometheus.GaugeVec
	licenseExpiry      *prometheus.GaugeVec
	upgradeInfo        *prometheus.GaugeVec
	upgradeSteps       *prometheus.GaugeVec
	upgradeStatusTime  *prometheus.GaugeVec
	dhcpStatsUtil      *prometheus.GaugeVec
	dhcpStatsStatus    *prometheus.GaugeVec
	dhcpStatsHosts     *prometheus.GaugeVec
	ipamStatsUtil      *prometheus.GaugeVec
	ipamStatsCount     *prometheus.GaugeVec
	ipamStatsUpdated   *prometheus.GaugeVec
	dhcpFailoverInfo   *prometheus.GaugeVec
	dhcpFailoverValue  *prometheus.GaugeVec
	dnsRecordInfo      *prometheus.GaugeVec
	dnsRecordTTL       *prometheus.GaugeVec
	dnsRecordCount     *prometheus.GaugeVec
	dnsRecordDisabled  *prometheus.GaugeVec
	dnsRecordReclaim   *prometheus.GaugeVec
	dnsZoneInfo        *prometheus.GaugeVec
	dtcObjectInfo      *prometheus.GaugeVec
	dtcObjectCount     *prometheus.GaugeVec
	threatStatValue    *prometheus.GaugeVec
}

type collectorResult struct {
	failed   bool
	blocking bool
}

type moduleState struct {
	Module       string        `json:"module"`
	LastAttempt  time.Time     `json:"-"`
	LastSuccess  time.Time     `json:"-"`
	LastDuration time.Duration `json:"-"`
	Attempts     uint64        `json:"attempts"`
	Errors       uint64        `json:"errors"`
	LastError    string        `json:"last_error,omitempty"`
}

type cacheStatus struct {
	Ready           bool                `json:"ready"`
	LastAttemptUnix int64               `json:"last_attempt_unix,omitempty"`
	LastSuccessUnix int64               `json:"last_success_unix,omitempty"`
	AgeSeconds      float64             `json:"age_seconds"`
	MaxStaleSeconds float64             `json:"max_stale_seconds"`
	Stale           bool                `json:"stale"`
	Modules         []cacheModuleStatus `json:"modules"`
}

type cacheModuleStatus struct {
	Module              string  `json:"module"`
	LastAttemptUnix     int64   `json:"last_attempt_unix,omitempty"`
	LastSuccessUnix     int64   `json:"last_success_unix,omitempty"`
	LastDurationSeconds float64 `json:"last_duration_seconds"`
	AgeSeconds          float64 `json:"age_seconds"`
	Stale               bool    `json:"stale"`
	Attempts            uint64  `json:"attempts"`
	Errors              uint64  `json:"errors"`
	LastError           string  `json:"last_error,omitempty"`
}

func New(cfg config.Config, client *wapi.Client, logger *slog.Logger) *Exporter {
	defaults := config.Default()
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = defaults.RefreshInterval
	}
	if cfg.RefreshTimeout <= 0 {
		cfg.RefreshTimeout = defaults.RefreshTimeout
	}
	if cfg.MaxStale <= 0 {
		cfg.MaxStale = defaults.MaxStale
	}
	namespace := "infoblox"
	g := func(name, help string, labels []string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: name, Help: help}, labels)
	}
	c := func(name, help string, labels []string) *prometheus.CounterVec {
		return prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: name, Help: help}, labels)
	}
	e := &Exporter{
		cfg:          cfg,
		client:       client,
		logger:       logger,
		moduleStates: make(map[string]*moduleState),

		up:                 g("up", "Whether the last Infoblox background refresh completed without collector errors.", nil),
		scrapeDuration:     g("scrape_duration_seconds", "Compatibility alias for the last Infoblox background refresh duration.", nil),
		collectorUp:        g("collector_up", "Whether the named Infoblox collector completed successfully during the last refresh.", []string{"collector"}),
		ipv4Configured:     g("ipv4address_collector_configured", "Whether the IPv4 address collector has explicit networks configured.", nil),
		refreshDuration:    g("refresh_duration_seconds", "Duration of the last Infoblox background refresh.", nil),
		refreshTotal:       c("refresh_total", "Total Infoblox background refresh attempts.", nil),
		refreshErrorsTotal: c("refresh_errors_total", "Total Infoblox background refresh failures.", nil),
		cacheLastAttempt:   g("cache_last_attempt_timestamp_seconds", "Unix timestamp of the last Infoblox cache refresh attempt.", nil),
		cacheLastSuccess:   g("cache_last_success_timestamp_seconds", "Unix timestamp of the last successful Infoblox cache refresh.", nil),
		cacheAge:           g("cache_age_seconds", "Age of the cached Infoblox data currently served.", nil),
		cacheStale:         g("cache_stale", "1 when the cached Infoblox data is stale or missing.", nil),

		networkInfo:        g("network_info", "Infoblox network metadata.", []string{"network", "network_view", "comment", "disabled"}),
		networkUtilization: g("network_utilization_ratio", "Infoblox IPAM network utilization ratio.", []string{"network", "network_view"}),
		networkDHCPUtil:    g("network_dhcp_utilization_ratio", "Infoblox network DHCP utilization ratio.", []string{"network", "network_view"}),
		networkUtilUpdated: g("network_utilization_updated_timestamp_seconds", "Timestamp when Infoblox network utilization was last updated.", []string{"network", "network_view"}),
		networkDHCPStatus:  g("network_dhcp_utilization_status", "Infoblox network DHCP utilization status as a one-hot gauge.", []string{"network", "network_view", "status"}),
		rangeInfo:          g("range_info", "Infoblox DHCP range metadata.", []string{"network", "network_view", "start_addr", "end_addr", "name", "comment", "disabled", "server_association_type", "failover_association"}),
		rangeDHCPUtil:      g("range_dhcp_utilization_ratio", "Infoblox DHCP range utilization ratio.", []string{"network", "network_view", "start_addr", "end_addr"}),
		rangeDHCPStatus:    g("range_dhcp_utilization_status", "Infoblox DHCP range utilization status as a one-hot gauge.", []string{"network", "network_view", "start_addr", "end_addr", "status"}),
		rangeDynamicHosts:  g("range_dynamic_hosts", "Total DHCP leases issued for the Infoblox range.", []string{"network", "network_view", "start_addr", "end_addr"}),
		ipv4StatusCount:    g("ipv4address_status_count", "Infoblox IPv4 address count by status.", []string{"network", "network_view", "status"}),
		ipv4TypeCount:      g("ipv4address_type_count", "Infoblox IPv4 address count by type.", []string{"network", "network_view", "type"}),
		ipv4UsageCount:     g("ipv4address_usage_count", "Infoblox IPv4 address count by usage.", []string{"network", "network_view", "usage"}),
		ipv4LeaseCount:     g("ipv4address_lease_state_count", "Infoblox IPv4 address count by lease state.", []string{"network", "network_view", "lease_state"}),
		ipv4ConflictCount:  g("ipv4address_conflicts", "Infoblox IPv4 addresses with conflict detected.", []string{"network", "network_view"}),
		memberInfo:         g("member_info", "Infoblox Grid member metadata.", []string{"member", "platform", "service_type_configuration"}),
		memberService:      g("member_service_status", "Infoblox Grid member service status as a one-hot gauge.", []string{"member", "service", "status"}),
		restartService:     g("restart_service_status", "Infoblox restart service status as a one-hot gauge.", []string{"member", "service", "status"}),
		serviceRestart:     g("service_restart_status_count", "Infoblox service restart status counts.", []string{"parent", "grouped", "state"}),
		serviceRestartReq:  g("service_restart_request_info", "Infoblox service restart request metadata.", []string{"member", "group", "service", "state", "needed", "result", "forced"}),
		serviceRestartTime: g("service_restart_request_updated_timestamp_seconds", "Timestamp when the Infoblox service restart request last changed.", []string{"member", "group", "service"}),
		capacityInfo:       g("capacity_info", "Infoblox member capacity metadata.", []string{"member", "role", "hardware_type"}),
		capacityUsed:       g("capacity_used_ratio", "Infoblox member object capacity usage ratio.", []string{"member", "role", "hardware_type"}),
		capacityMax:        g("capacity_max_objects", "Infoblox member maximum object capacity.", []string{"member", "role", "hardware_type"}),
		capacityObjects:    g("capacity_objects", "Infoblox member total object count.", []string{"member", "role", "hardware_type"}),
		capacityObjectType: g("capacity_object_type_count", "Infoblox member object count by object type.", []string{"member", "object_type"}),
		licenseInfo:        g("license_info", "Infoblox license metadata without license key material.", []string{"scope", "type", "kind", "limit", "limit_context", "expiration_status", "hwid"}),
		licenseExpiry:      g("license_expiry_timestamp_seconds", "Infoblox license expiry timestamp.", []string{"scope", "type", "kind", "limit", "limit_context", "hwid"}),
		upgradeInfo:        g("upgrade_status_info", "Infoblox upgrade status metadata.", []string{"type", "member", "upgrade_group", "element_status", "grid_state", "group_state", "ha_status", "status_value", "upgrade_state", "upgrade_test_status", "current_version", "distribution_version", "upload_version", "reverted"}),
		upgradeSteps:       g("upgrade_steps", "Infoblox upgrade step counters.", []string{"type", "member", "upgrade_group", "kind"}),
		upgradeStatusTime:  g("upgrade_status_updated_timestamp_seconds", "Timestamp when the Infoblox upgrade status value was updated.", []string{"type", "member", "upgrade_group"}),
		dhcpStatsUtil:      g("dhcp_statistics_utilization_ratio", "Infoblox DHCP statistics utilization ratio.", []string{"object_type", "object"}),
		dhcpStatsStatus:    g("dhcp_statistics_utilization_status", "Infoblox DHCP statistics status as a one-hot gauge.", []string{"object_type", "object", "status"}),
		dhcpStatsHosts:     g("dhcp_statistics_hosts", "Infoblox DHCP statistics host counts.", []string{"object_type", "object", "kind"}),
		ipamStatsUtil:      g("ipam_statistics_utilization_ratio", "Infoblox IPAM statistics utilization ratio.", []string{"network", "network_view"}),
		ipamStatsCount:     g("ipam_statistics_count", "Infoblox IPAM statistics counts.", []string{"network", "network_view", "kind"}),
		ipamStatsUpdated:   g("ipam_statistics_updated_timestamp_seconds", "Timestamp when Infoblox IPAM utilization was last updated.", []string{"network", "network_view"}),
		dhcpFailoverInfo:   g("dhcp_failover_info", "Infoblox DHCP failover association metadata.", []string{"name", "association_type", "comment", "primary", "secondary", "primary_server_type", "secondary_server_type"}),
		dhcpFailoverValue:  g("dhcp_failover_value", "Infoblox DHCP failover numeric settings.", []string{"name", "setting"}),
		dnsRecordInfo:      g("dns_record_info", "Infoblox DNS record metadata. This can be high-cardinality.", []string{"view", "zone", "type", "name", "disabled", "reclaimable"}),
		dnsRecordTTL:       g("dns_record_ttl_seconds", "Infoblox DNS record TTL in seconds. This can be high-cardinality.", []string{"view", "zone", "type", "name"}),
		dnsRecordCount:     g("dns_record_count", "Infoblox DNS record count by view, zone, and type.", []string{"view", "zone", "type"}),
		dnsRecordDisabled:  g("dns_record_disabled_count", "Infoblox disabled DNS record count by view, zone, and type.", []string{"view", "zone", "type"}),
		dnsRecordReclaim:   g("dns_record_reclaimable_count", "Infoblox reclaimable DNS record count by view, zone, and type.", []string{"view", "zone", "type"}),
		dnsZoneInfo:        g("dns_zone_info", "Infoblox DNS zone metadata.", []string{"view", "zone", "type", "comment", "disabled"}),
		dtcObjectInfo:      g("dtc_object_info", "Infoblox DTC object metadata.", []string{"name", "abstract_type", "display_type", "status", "comment"}),
		dtcObjectCount:     g("dtc_object_count", "Infoblox DTC object count by type and status.", []string{"abstract_type", "display_type", "status"}),
		threatStatValue:    g("threatprotection_stat_value", "Infoblox threat protection numeric statistic values.", []string{"member", "stat"}),
	}
	e.up.WithLabelValues().Set(0)
	e.cacheStale.WithLabelValues().Set(1)
	e.cacheAge.WithLabelValues().Set(0)
	e.cacheLastAttempt.WithLabelValues().Set(0)
	e.cacheLastSuccess.WithLabelValues().Set(0)
	e.refreshDuration.WithLabelValues().Set(0)
	e.scrapeDuration.WithLabelValues().Set(0)
	e.ipv4Configured.WithLabelValues().Set(0)
	return e
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, collector := range e.allCollectors() {
		collector.Describe(ch)
	}
}

// Collect serves only cached metrics. All Infoblox WAPI I/O happens in RefreshOnce.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.cacheMu.Lock()
	e.updateDynamicCacheMetricsLocked(time.Now())
	collectors := e.allCollectors()
	e.cacheMu.Unlock()

	for _, collector := range collectors {
		collector.Collect(ch)
	}
}

func (e *Exporter) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	e.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		e.schedulerCancel = cancel
		go e.schedulerLoop(runCtx)
	})
}

func (e *Exporter) Stop() {
	if e.schedulerCancel != nil {
		e.schedulerCancel()
	}
}

func (e *Exporter) schedulerLoop(ctx context.Context) {
	e.runRefreshWithLog(ctx)
	ticker := time.NewTicker(e.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.runRefreshWithLog(ctx)
		}
	}
}

func (e *Exporter) runRefreshWithLog(ctx context.Context) {
	if err := e.RefreshOnce(ctx); err != nil && e.logger != nil {
		e.logger.Warn("background refresh completed with errors", "err", err)
	}
}

func (e *Exporter) RefreshOnce(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()

	refreshTimeout := e.cfg.RefreshTimeout
	if refreshTimeout <= 0 {
		refreshTimeout = e.cfg.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	start := time.Now()
	e.cacheMu.Lock()
	e.refreshTotal.WithLabelValues().Inc()
	e.cacheLastAttempt.WithLabelValues().Set(float64(start.Unix()))
	e.cacheMu.Unlock()

	next := New(e.cfg, e.client, e.logger)
	errorCount := 0
	blockingErrorCount := 0
	recordResult := func(result collectorResult) {
		if !result.failed {
			return
		}
		errorCount++
		if result.blocking {
			blockingErrorCount++
		}
	}
	if next.enabled("network") {
		recordResult(next.runCollector(ctx, "network", next.collectNetworks))
	}
	if next.enabled("range") {
		recordResult(next.runCollector(ctx, "range", next.collectRanges))
	}
	if next.enabled("ipv4address") {
		configured := 0.0
		if len(next.cfg.Networks) > 0 {
			configured = 1
			recordResult(next.runCollector(ctx, "ipv4address", next.collectIPv4Addresses))
		} else {
			next.collectorUp.WithLabelValues("ipv4address").Set(1)
		}
		next.ipv4Configured.WithLabelValues().Set(configured)
	}
	if next.enabled("member") {
		recordResult(next.runCollector(ctx, "member", next.collectMembers))
	}
	if next.enabled("restartservicestatus") {
		recordResult(next.runCollector(ctx, "restartservicestatus", next.collectRestartServiceStatus))
	}
	if next.enabled("servicerestart") {
		recordResult(next.runCollector(ctx, "servicerestart", next.collectServiceRestart))
	}
	if next.enabled("capacity") {
		recordResult(next.runCollector(ctx, "capacity", next.collectCapacity))
	}
	if next.enabled("license") {
		recordResult(next.runCollector(ctx, "license", next.collectLicenses))
	}
	if next.enabled("upgradestatus") {
		recordResult(next.runCollector(ctx, "upgradestatus", next.collectUpgradeStatus))
	}
	if next.enabled("dhcpstatistics") {
		recordResult(next.runCollector(ctx, "dhcpstatistics", next.collectDHCPStatistics))
	}
	if next.enabled("ipamstatistics") {
		recordResult(next.runCollector(ctx, "ipamstatistics", next.collectIPAMStatistics))
	}
	if next.enabled("dhcpfailover") {
		recordResult(next.runCollector(ctx, "dhcpfailover", next.collectDHCPFailover))
	}
	if next.enabled("allrecords") {
		recordResult(next.runCollector(ctx, "allrecords", next.collectAllRecords))
	}
	if next.enabled("zones") {
		recordResult(next.runCollector(ctx, "zones", next.collectZones))
	}
	if next.enabled("dtc") {
		recordResult(next.runCollector(ctx, "dtc", next.collectDTC))
	}
	if next.enabled("threatprotection") {
		recordResult(next.runCollector(ctx, "threatprotection", next.collectThreatProtection))
	}

	up := 1.0
	if errorCount > 0 {
		up = 0
	}
	duration := time.Since(start)
	finished := time.Now()
	e.cacheMu.Lock()
	e.up.WithLabelValues().Set(up)
	e.scrapeDuration.WithLabelValues().Set(duration.Seconds())
	e.refreshDuration.WithLabelValues().Set(duration.Seconds())
	if blockingErrorCount == 0 {
		e.replaceCachedDataLocked(next)
		e.cacheLastSuccess.WithLabelValues().Set(float64(finished.Unix()))
	}
	if errorCount > 0 {
		e.refreshErrorsTotal.WithLabelValues().Inc()
	}
	e.updateDynamicCacheMetricsLocked(finished)
	e.cacheMu.Unlock()
	if errorCount > 0 {
		return fmt.Errorf("%d collector(s) failed", errorCount)
	}
	return nil
}

func (e *Exporter) enabled(name string) bool {
	return !e.cfg.IsModuleDisabled(name)
}

func (e *Exporter) runCollector(parent context.Context, name string, fn func(context.Context, chan<- prometheus.Metric) error) collectorResult {
	ctx, cancel := context.WithTimeout(parent, e.cfg.Timeout)
	defer cancel()

	start := time.Now()
	err := fn(ctx, nil)
	finished := time.Now()
	duration := finished.Sub(start)

	e.cacheMu.Lock()
	state := e.ensureModuleStateLocked(name)
	state.LastAttempt = finished
	state.LastDuration = duration
	state.Attempts++
	if err != nil {
		state.Errors++
		state.LastError = err.Error()
		e.collectorUp.WithLabelValues(name).Set(0)
	} else {
		state.LastSuccess = finished
		state.LastError = ""
		e.collectorUp.WithLabelValues(name).Set(1)
	}
	e.cacheMu.Unlock()

	if err != nil {
		e.logger.Warn("collector failed", "collector", name, "err", err)
		return collectorResult{failed: true, blocking: collectorFailureBlocksRefresh(name)}
	}
	return collectorResult{}
}

func collectorFailureBlocksRefresh(name string) bool {
	switch name {
	case "network", "range", "ipv4address", "member":
		return true
	default:
		return false
	}
}

func (e *Exporter) ensureModuleStateLocked(name string) *moduleState {
	if state := e.moduleStates[name]; state != nil {
		return state
	}
	state := &moduleState{Module: name}
	e.moduleStates[name] = state
	return state
}

func (e *Exporter) allCollectors() []prometheus.Collector {
	collectors := []prometheus.Collector{
		e.up, e.scrapeDuration, e.collectorUp, e.ipv4Configured,
		e.refreshDuration, e.refreshTotal, e.refreshErrorsTotal,
		e.cacheLastAttempt, e.cacheLastSuccess, e.cacheAge, e.cacheStale,
	}
	for _, gauge := range e.dataGaugeVecs() {
		collectors = append(collectors, gauge)
	}
	return collectors
}

func (e *Exporter) dataGaugeVecs() []*prometheus.GaugeVec {
	return []*prometheus.GaugeVec{
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
	}
}

func (e *Exporter) replaceCachedDataLocked(next *Exporter) {
	e.collectorUp = next.collectorUp
	e.ipv4Configured = next.ipv4Configured
	e.moduleStates = next.moduleStates

	e.networkInfo = next.networkInfo
	e.networkUtilization = next.networkUtilization
	e.networkDHCPUtil = next.networkDHCPUtil
	e.networkUtilUpdated = next.networkUtilUpdated
	e.networkDHCPStatus = next.networkDHCPStatus
	e.rangeInfo = next.rangeInfo
	e.rangeDHCPUtil = next.rangeDHCPUtil
	e.rangeDHCPStatus = next.rangeDHCPStatus
	e.rangeDynamicHosts = next.rangeDynamicHosts
	e.ipv4StatusCount = next.ipv4StatusCount
	e.ipv4TypeCount = next.ipv4TypeCount
	e.ipv4UsageCount = next.ipv4UsageCount
	e.ipv4LeaseCount = next.ipv4LeaseCount
	e.ipv4ConflictCount = next.ipv4ConflictCount
	e.memberInfo = next.memberInfo
	e.memberService = next.memberService
	e.restartService = next.restartService
	e.serviceRestart = next.serviceRestart
	e.serviceRestartReq = next.serviceRestartReq
	e.serviceRestartTime = next.serviceRestartTime
	e.capacityInfo = next.capacityInfo
	e.capacityUsed = next.capacityUsed
	e.capacityMax = next.capacityMax
	e.capacityObjects = next.capacityObjects
	e.capacityObjectType = next.capacityObjectType
	e.licenseInfo = next.licenseInfo
	e.licenseExpiry = next.licenseExpiry
	e.upgradeInfo = next.upgradeInfo
	e.upgradeSteps = next.upgradeSteps
	e.upgradeStatusTime = next.upgradeStatusTime
	e.dhcpStatsUtil = next.dhcpStatsUtil
	e.dhcpStatsStatus = next.dhcpStatsStatus
	e.dhcpStatsHosts = next.dhcpStatsHosts
	e.ipamStatsUtil = next.ipamStatsUtil
	e.ipamStatsCount = next.ipamStatsCount
	e.ipamStatsUpdated = next.ipamStatsUpdated
	e.dhcpFailoverInfo = next.dhcpFailoverInfo
	e.dhcpFailoverValue = next.dhcpFailoverValue
	e.dnsRecordInfo = next.dnsRecordInfo
	e.dnsRecordTTL = next.dnsRecordTTL
	e.dnsRecordCount = next.dnsRecordCount
	e.dnsRecordDisabled = next.dnsRecordDisabled
	e.dnsRecordReclaim = next.dnsRecordReclaim
	e.dnsZoneInfo = next.dnsZoneInfo
	e.dtcObjectInfo = next.dtcObjectInfo
	e.dtcObjectCount = next.dtcObjectCount
	e.threatStatValue = next.threatStatValue
}

func (e *Exporter) updateDynamicCacheMetricsLocked(now time.Time) {
	status := e.cacheStatusLocked(now)
	e.cacheAge.WithLabelValues().Set(status.AgeSeconds)
	if status.Stale {
		e.cacheStale.WithLabelValues().Set(1)
	} else {
		e.cacheStale.WithLabelValues().Set(0)
	}
}

func (e *Exporter) Ready() bool {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	return e.cacheStatusLocked(time.Now()).Ready
}

func (e *Exporter) CacheStatus() cacheStatus {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	return e.cacheStatusLocked(time.Now())
}

func (e *Exporter) cacheStatusLocked(now time.Time) cacheStatus {
	lastAttempt := timestampFromGauge(e.cacheLastAttempt)
	lastSuccess := timestampFromGauge(e.cacheLastSuccess)
	stale := lastSuccess.IsZero() || now.Sub(lastSuccess) > e.cfg.MaxStale
	status := cacheStatus{
		Ready:           !stale,
		AgeSeconds:      0,
		MaxStaleSeconds: e.cfg.MaxStale.Seconds(),
		Stale:           stale,
		Modules:         make([]cacheModuleStatus, 0, len(e.moduleStates)),
	}
	if !lastAttempt.IsZero() {
		status.LastAttemptUnix = lastAttempt.Unix()
	}
	if !lastSuccess.IsZero() {
		status.LastSuccessUnix = lastSuccess.Unix()
		status.AgeSeconds = now.Sub(lastSuccess).Seconds()
	}
	for _, state := range e.moduleStates {
		module := cacheModuleStatus{
			Module:              state.Module,
			LastDurationSeconds: state.LastDuration.Seconds(),
			Stale:               state.LastSuccess.IsZero() || now.Sub(state.LastSuccess) > e.cfg.MaxStale,
			Attempts:            state.Attempts,
			Errors:              state.Errors,
			LastError:           state.LastError,
		}
		if !state.LastAttempt.IsZero() {
			module.LastAttemptUnix = state.LastAttempt.Unix()
		}
		if !state.LastSuccess.IsZero() {
			module.LastSuccessUnix = state.LastSuccess.Unix()
			module.AgeSeconds = now.Sub(state.LastSuccess).Seconds()
		}
		status.Modules = append(status.Modules, module)
	}
	sort.Slice(status.Modules, func(i, j int) bool {
		return status.Modules[i].Module < status.Modules[j].Module
	})
	return status
}

func (e *Exporter) ReadyHandler(w http.ResponseWriter, _ *http.Request) {
	if e.Ready() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK\n"))
		return
	}
	http.Error(w, "cache is not ready", http.StatusServiceUnavailable)
}

func (e *Exporter) DebugCacheHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(e.CacheStatus())
}

func timestampFromGauge(gauge *prometheus.GaugeVec) time.Time {
	metric := &dto.Metric{}
	if err := gauge.WithLabelValues().Write(metric); err != nil || metric.Gauge == nil || metric.Gauge.Value == nil || *metric.Gauge.Value == 0 {
		return time.Time{}
	}
	return time.Unix(int64(*metric.Gauge.Value), 0)
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
		e.networkInfo.WithLabelValues(item.Network, networkView, item.Comment, boolLabel(item.Disable)).Set(1)
		if item.Utilization.Valid {
			e.networkUtilization.WithLabelValues(item.Network, networkView).Set(ipamUtilizationRatio(item.Utilization.Value))
		}
		if item.DHCPUtilization.Valid {
			e.networkDHCPUtil.WithLabelValues(item.Network, networkView).Set(dhcpUtilizationRatio(item.DHCPUtilization.Value))
		}
		if item.UtilizationUpdate.Valid {
			e.networkUtilUpdated.WithLabelValues(item.Network, networkView).Set(float64(item.UtilizationUpdate.Value))
		}
		if item.DHCPUtilizationStatus != "" {
			e.networkDHCPStatus.WithLabelValues(item.Network, networkView, item.DHCPUtilizationStatus).Set(1)
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
		e.rangeInfo.WithLabelValues(
			item.Network,
			networkView,
			item.StartAddr,
			item.EndAddr,
			item.Name,
			item.Comment,
			boolLabel(item.Disable),
			item.ServerAssociationType,
			item.FailoverAssociation,
		).Set(1)
		if item.DHCPUtilization.Valid {
			e.rangeDHCPUtil.WithLabelValues(item.Network, networkView, item.StartAddr, item.EndAddr).Set(dhcpUtilizationRatio(item.DHCPUtilization.Value))
		}
		if item.DHCPUtilizationStatus != "" {
			e.rangeDHCPStatus.WithLabelValues(item.Network, networkView, item.StartAddr, item.EndAddr, item.DHCPUtilizationStatus).Set(1)
		}
		if item.DynamicHosts.Valid {
			e.rangeDynamicHosts.WithLabelValues(item.Network, networkView, item.StartAddr, item.EndAddr).Set(float64(item.DynamicHosts.Value))
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

	emitCounts(e.ipv4StatusCount, statusCounts, network, networkView)
	emitCounts(e.ipv4TypeCount, typeCounts, network, networkView)
	emitCounts(e.ipv4UsageCount, usageCounts, network, networkView)
	emitCounts(e.ipv4LeaseCount, leaseCounts, network, networkView)
	e.ipv4ConflictCount.WithLabelValues(network, networkView).Set(float64(conflicts))
	return nil
}

func (e *Exporter) collectMembers(ctx context.Context, ch chan<- prometheus.Metric) error {
	params := fields("host_name", "platform", "service_type_configuration", "service_status")
	members, err := wapi.FetchAll[model.Member](ctx, e.client, "member", params)
	if err != nil {
		return err
	}
	for _, member := range members {
		e.memberInfo.WithLabelValues(member.HostName, member.Platform, member.ServiceTypeConfiguration).Set(1)
		for _, rawStatus := range member.ServiceStatus {
			service, status := memberServiceStatus(rawStatus)
			if service == "" || status == "" {
				continue
			}
			e.memberService.WithLabelValues(member.HostName, service, status).Set(1)
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
		emitStatus(e.restartService, status.Member, "dhcp", status.DHCPStatus)
		emitStatus(e.restartService, status.Member, "dns", status.DNSStatus)
		emitStatus(e.restartService, status.Member, "reporting", status.ReportingStatus)
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
		emitUint(e.serviceRestart, status.Failures, parent, grouped, "failures")
		emitUint(e.serviceRestart, status.Finished, parent, grouped, "finished")
		emitUint(e.serviceRestart, status.NeededRestart, parent, grouped, "needed_restart")
		emitUint(e.serviceRestart, status.NoRestart, parent, grouped, "no_restart")
		emitUint(e.serviceRestart, status.Pending, parent, grouped, "pending")
		emitUint(e.serviceRestart, status.PendingRestart, parent, grouped, "pending_restart")
		emitUint(e.serviceRestart, status.Processing, parent, grouped, "processing")
		emitUint(e.serviceRestart, status.Restarting, parent, grouped, "restarting")
		emitUint(e.serviceRestart, status.Success, parent, grouped, "success")
		emitUint(e.serviceRestart, status.Timeouts, parent, grouped, "timeouts")
	}

	reqParams := fields("member", "group", "service", "state", "needed", "result", "forced", "last_updated_time", "error")
	requests, err := wapi.FetchAll[model.ServiceRestartRequest](ctx, e.client, "grid:servicerestart:request", reqParams)
	if err != nil {
		return err
	}
	for _, request := range requests {
		e.serviceRestartReq.WithLabelValues(
			request.Member,
			request.Group,
			request.Service,
			request.State,
			request.Needed,
			request.Result,
			boolLabel(request.Forced),
		).Set(1)
		if request.LastUpdatedTime.Valid {
			e.serviceRestartTime.WithLabelValues(request.Member, request.Group, request.Service).Set(float64(request.LastUpdatedTime.Value))
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
			if isCapacityReportScopeError(err) {
				if e.logger != nil {
					e.logger.Debug("capacity report skipped for member", "member", memberName, "err", err)
				}
				continue
			}
			return err
		}
		for _, report := range reports {
			e.capacityInfo.WithLabelValues(report.Name, report.Role, report.HardwareType).Set(1)
			if report.PercentUsed.Valid {
				e.capacityUsed.WithLabelValues(report.Name, report.Role, report.HardwareType).Set(utilizationRatio(report.PercentUsed.Value))
			}
			emitUint(e.capacityMax, report.MaxCapacity, report.Name, report.Role, report.HardwareType)
			emitUint(e.capacityObjects, report.TotalObjects, report.Name, report.Role, report.HardwareType)
			for _, count := range report.ObjectCounts {
				objectType := firstString(count, "type", "type_name", "object_type", "name")
				value, ok := firstNumber(count, "count", "total", "value")
				if objectType == "" || !ok {
					continue
				}
				e.capacityObjectType.WithLabelValues(report.Name, objectType).Set(value)
			}
		}
	}
	return nil
}

func isCapacityReportScopeError(err error) bool {
	var wapiErr wapi.WAPIError
	return errors.As(err, &wapiErr) &&
		wapiErr.StatusCode == http.StatusBadRequest &&
		wapiErr.Code == "Client.Ibap.Data" &&
		strings.Contains(wapiErr.Text, "GMC can only retrieve the capacity report for itself")
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
	e.licenseInfo.WithLabelValues(
		license.Scope,
		license.Type,
		license.Kind,
		license.Limit,
		license.LimitContext,
		license.ExpirationStatus,
		license.HWID,
	).Set(1)
	if license.ExpiryDate.Valid {
		e.licenseExpiry.WithLabelValues(license.Scope, license.Type, license.Kind, license.Limit, license.LimitContext, license.HWID).Set(float64(license.ExpiryDate.Value))
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
			e.upgradeInfo.WithLabelValues(
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
			).Set(1)
			emitUint(e.upgradeSteps, status.StepsCompleted, statusType, status.Member, status.UpgradeGroup, "completed")
			emitUint(e.upgradeSteps, status.StepsTotal, statusType, status.Member, status.UpgradeGroup, "total")
			if status.StatusValueUpdateTime.Valid {
				e.upgradeStatusTime.WithLabelValues(statusType, status.Member, status.UpgradeGroup).Set(float64(status.StatusValueUpdateTime.Value))
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
		objectCtx, cancel := context.WithTimeout(ctx, e.dhcpStatisticsObjectTimeout())
		stats, err := wapi.FetchAllUnpaged[model.DHCPStatistics](objectCtx, e.client, "dhcp:statistics", query)
		cancel()
		if err != nil {
			switch {
			case errors.Is(ctx.Err(), context.Canceled):
				return ctx.Err()
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				e.logger.Debug("dhcp statistics collector deadline reached", "object_type", object.kind, "object", object.name, "ref", object.ref, "err", err)
				return nil
			case isTimeoutError(err):
				e.logger.Debug("dhcp statistics object timed out", "object_type", object.kind, "object", object.name, "ref", object.ref, "err", err)
				continue
			}
			return err
		}
		for _, stat := range stats {
			if stat.DHCPUtilization.Valid {
				e.dhcpStatsUtil.WithLabelValues(object.kind, object.name).Set(dhcpUtilizationRatio(stat.DHCPUtilization.Value))
			}
			if stat.DHCPUtilizationStatus != "" {
				e.dhcpStatsStatus.WithLabelValues(object.kind, object.name, stat.DHCPUtilizationStatus).Set(1)
			}
			emitUint(e.dhcpStatsHosts, stat.DynamicHosts, object.kind, object.name, "dynamic")
			emitUint(e.dhcpStatsHosts, stat.StaticHosts, object.kind, object.name, "static")
			emitUint(e.dhcpStatsHosts, stat.TotalHosts, object.kind, object.name, "total")
		}
	}
	return nil
}

func (e *Exporter) dhcpStatisticsObjectTimeout() time.Duration {
	if e.cfg.Timeout <= 0 {
		return 5 * time.Second
	}
	timeout := e.cfg.Timeout / 6
	if timeout <= 0 {
		return e.cfg.Timeout
	}
	if timeout > 5*time.Second {
		return 5 * time.Second
	}
	return timeout
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
	fieldNames := []string{"network", "network_view", "cidr", "utilization"}
	if network != "" {
		fieldNames = append(fieldNames, "utilization_update")
	}
	params := fields(fieldNames...)
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
			e.ipamStatsUtil.WithLabelValues(stat.Network, networkView).Set(ipamUtilizationRatio(stat.Utilization.Value))
		}
		emitUint(e.ipamStatsCount, stat.ConflictCount, stat.Network, networkView, "conflict")
		emitUint(e.ipamStatsCount, stat.UnmanagedCount, stat.Network, networkView, "unmanaged")
		if stat.UtilizationUpdate.Valid {
			e.ipamStatsUpdated.WithLabelValues(stat.Network, networkView).Set(float64(stat.UtilizationUpdate.Value))
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
		e.dhcpFailoverInfo.WithLabelValues(
			failover.Name,
			failover.AssociationType,
			failover.Comment,
			failover.Primary,
			failover.Secondary,
			failover.PrimaryServerType,
			failover.SecondaryServerType,
		).Set(1)
		emitUint(e.dhcpFailoverValue, failover.LoadBalanceSplit, failover.Name, "load_balance_split")
		emitUint(e.dhcpFailoverValue, failover.MaxClientLeadTime, failover.Name, "max_client_lead_time")
		emitUint(e.dhcpFailoverValue, failover.MaxResponseDelay, failover.Name, "max_response_delay")
		emitUint(e.dhcpFailoverValue, failover.MaxUnackedUpdates, failover.Name, "max_unacked_updates")
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
		e.dnsRecordInfo.WithLabelValues(recordView, recordZone, recordType, record.Name, boolLabel(record.Disable), boolLabel(record.Reclaimable)).Set(1)
		if record.TTL.Valid {
			e.dnsRecordTTL.WithLabelValues(recordView, recordZone, recordType, record.Name).Set(float64(record.TTL.Value))
		}
	}
	emitRecordCounts(e.dnsRecordCount, counts)
	emitRecordCounts(e.dnsRecordDisabled, disabled)
	emitRecordCounts(e.dnsRecordReclaim, reclaimable)
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
			e.dnsZoneInfo.WithLabelValues(valueOr(zone.View, view, "default"), zone.FQDN, objectType, zone.Comment, boolLabel(zone.Disable)).Set(1)
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
		e.dtcObjectInfo.WithLabelValues(object.Name, object.AbstractType, object.DisplayType, object.Status, object.Comment).Set(1)
		key := recordKey(valueOr(object.AbstractType, "unknown"), valueOr(object.DisplayType, "unknown"), valueOr(object.Status, "unknown"))
		counts[key]++
	}
	emitRecordCounts(e.dtcObjectCount, counts)
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
			e.threatStatValue.WithLabelValues(member, key).Set(values[key])
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

func dhcpUtilizationRatio(value uint64) float64 {
	return float64(value) / 1000
}

func ipamUtilizationRatio(value uint64) float64 {
	return float64(value) / 1000
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func emitCounts(gauge *prometheus.GaugeVec, counts map[string]int, network string, networkView string) {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		gauge.WithLabelValues(network, networkView, key).Set(float64(counts[key]))
	}
}

func emitStatus(gauge *prometheus.GaugeVec, labels ...string) {
	for _, label := range labels {
		if label == "" {
			return
		}
	}
	gauge.WithLabelValues(labels...).Set(1)
}

func emitUint(gauge *prometheus.GaugeVec, value model.Uint64, labels ...string) {
	if !value.Valid {
		return
	}
	gauge.WithLabelValues(labels...).Set(float64(value.Value))
}

func emitRecordCounts(gauge *prometheus.GaugeVec, counts map[string]int) {
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
		gauge.WithLabelValues(parts[0], parts[1], parts[2]).Set(float64(counts[key]))
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
