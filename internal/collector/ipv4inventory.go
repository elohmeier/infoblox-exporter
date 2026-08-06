package collector

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/elohmeier/infoblox-exporter/internal/model"
	"github.com/elohmeier/infoblox-exporter/internal/wapi"
	"github.com/prometheus/client_golang/prometheus"
)

type inventoryAddress struct {
	IPAddress   string
	Network     string
	NetworkView string
	Names       string
	Types       string
	Usage       string
}

type inventoryNetworkSelector struct {
	anyView map[string]struct{}
	byView  map[string]map[string]struct{}
}

type inventoryInterval struct {
	start uint32
	end   uint32
}

type inventoryQuery struct {
	view     string
	interval inventoryInterval
	bounded  bool
}

func (e *Exporter) collectIPv4Inventory(ctx context.Context, _ chan<- prometheus.Metric) error {
	selector, err := e.inventoryNetworkSelector(ctx)
	if err != nil {
		return err
	}
	queries, err := e.inventoryQueries(selector)
	if err != nil {
		return err
	}

	selectedCount := selector.count()
	e.ipv4InventorySelected.WithLabelValues().Set(float64(selectedCount))
	e.ipv4InventoryScanRanges.WithLabelValues().Set(float64(uniqueBoundedIntervals(queries)))

	seen := make(map[string]struct{})
	addresses := make([]inventoryAddress, 0, min(selectedCount*8, e.cfg.IPv4InventoryMaxAddresses))
	returned := 0
	filtered := 0
	for _, query := range queries {
		params := fields("ip_address", "network", "network_view", "status", "names", "types", "usage")
		params.Set("status", "USED")
		params.Set("_max_results", strconv.Itoa(e.cfg.IPv4InventoryPageSize))
		if query.view != "" {
			params.Set("network_view", query.view)
		}
		if e.cfg.IPv4InventoryNameRegex != "" {
			params.Set("names~", e.cfg.IPv4InventoryNameRegex)
		}
		if query.bounded {
			setInventoryBounds(params, query.interval)
		}

		err := wapi.FetchPages[model.IPv4Address](ctx, e.client, "ipv4address", params, func(page []model.IPv4Address) error {
			returned += len(page)
			for _, item := range page {
				ip, err := netip.ParseAddr(item.IPAddress)
				if err != nil || !ip.Is4() || !strings.EqualFold(item.Status, "USED") {
					continue
				}
				ipValue := ipv4Uint32(ip)
				if query.bounded && (ipValue < query.interval.start || ipValue > query.interval.end) {
					continue
				}
				networkPrefix, err := parseIPv4Prefix(item.Network)
				if err != nil {
					continue
				}
				network := networkPrefix.String()
				view := valueOr(item.NetworkView, query.view, "default")
				if selector.configured() && !selector.matches(network, view) {
					continue
				}
				filtered++
				key := view + "\x00" + ip.String()
				if _, exists := seen[key]; exists {
					continue
				}
				if len(addresses) >= e.cfg.IPv4InventoryMaxAddresses {
					return fmt.Errorf("IPv4 inventory exceeds configured maximum of %d occupied addresses", e.cfg.IPv4InventoryMaxAddresses)
				}
				seen[key] = struct{}{}
				addresses = append(addresses, inventoryAddress{
					IPAddress:   ip.String(),
					Network:     network,
					NetworkView: view,
					Names:       canonicalLabelValues(item.Names),
					Types:       canonicalLabelValues(item.Types),
					Usage:       canonicalLabelValues(item.Usage),
				})
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	sort.Slice(addresses, func(i, j int) bool {
		if addresses[i].NetworkView != addresses[j].NetworkView {
			return addresses[i].NetworkView < addresses[j].NetworkView
		}
		left, _ := netip.ParseAddr(addresses[i].IPAddress)
		right, _ := netip.ParseAddr(addresses[j].IPAddress)
		if left != right {
			return left.Less(right)
		}
		return addresses[i].Network < addresses[j].Network
	})

	counts := make(map[string]int)
	for _, address := range addresses {
		counts[address.NetworkView+"\x00"+address.Network]++
	}
	for key, count := range counts {
		parts := strings.SplitN(key, "\x00", 2)
		e.ipv4InventoryAddressCount.WithLabelValues(parts[1], parts[0]).Set(float64(count))
	}
	e.ipv4InventoryObjects.WithLabelValues("returned").Set(float64(returned))
	e.ipv4InventoryObjects.WithLabelValues("filtered").Set(float64(filtered))
	e.ipv4InventoryObjects.WithLabelValues("emitted").Set(float64(len(addresses)))
	e.ipv4Inventory = addresses
	return nil
}

func (e *Exporter) inventoryNetworkSelector(ctx context.Context) (*inventoryNetworkSelector, error) {
	selector := &inventoryNetworkSelector{
		anyView: make(map[string]struct{}),
		byView:  make(map[string]map[string]struct{}),
	}
	for _, value := range e.cfg.IPv4InventoryNetworks {
		prefix, err := parseIPv4Prefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid IPv4 inventory network %q: %w", value, err)
		}
		selector.anyView[prefix.String()] = struct{}{}
	}

	if e.cfg.IPv4InventoryNetworkEA == "" {
		return selector, nil
	}
	parts := strings.SplitN(e.cfg.IPv4InventoryNetworkEA, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, fmt.Errorf("invalid IPv4 inventory network EA %q; expected name=value", e.cfg.IPv4InventoryNetworkEA)
	}
	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	for _, view := range viewsOrSingleEmpty(e.cfg.NetworkViews) {
		params := fields("network", "network_view")
		params.Set("*"+name, value)
		params.Set("_max_results", strconv.Itoa(e.cfg.IPv4InventoryPageSize))
		if view != "" {
			params.Set("network_view", view)
		}
		if err := wapi.FetchPages[model.Network](ctx, e.client, "network", params, func(page []model.Network) error {
			for _, item := range page {
				prefix, err := parseIPv4Prefix(item.Network)
				if err != nil {
					return fmt.Errorf("Infoblox returned invalid IPv4 network %q: %w", item.Network, err)
				}
				networkView := valueOr(item.NetworkView, view, "default")
				if selector.byView[networkView] == nil {
					selector.byView[networkView] = make(map[string]struct{})
				}
				selector.byView[networkView][prefix.String()] = struct{}{}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return selector, nil
}

func (e *Exporter) inventoryQueries(selector *inventoryNetworkSelector) ([]inventoryQuery, error) {
	selectionConfigured := len(e.cfg.IPv4InventoryNetworks) > 0 || e.cfg.IPv4InventoryNetworkEA != ""
	if selectionConfigured && selector.count() == 0 {
		return nil, nil
	}

	if len(e.cfg.IPv4InventoryScanRanges) > 0 {
		intervals, err := mergedInventoryIntervals(e.cfg.IPv4InventoryScanRanges)
		if err != nil {
			return nil, err
		}
		return inventoryQueriesForViews(viewsOrSingleEmpty(e.cfg.NetworkViews), intervals), nil
	}

	if selector.count() == 0 {
		if e.cfg.IPv4InventoryNameRegex == "" {
			return nil, nil
		}
		queries := make([]inventoryQuery, 0, max(1, len(e.cfg.NetworkViews)))
		for _, view := range viewsOrSingleEmpty(e.cfg.NetworkViews) {
			queries = append(queries, inventoryQuery{view: view})
		}
		return queries, nil
	}

	if len(e.cfg.NetworkViews) == 0 {
		intervals, err := mergedInventoryIntervals(selector.allNetworks())
		if err != nil {
			return nil, err
		}
		return inventoryQueriesForViews([]string{""}, intervals), nil
	}

	var queries []inventoryQuery
	for _, view := range e.cfg.NetworkViews {
		intervals, err := mergedInventoryIntervals(selector.networksForView(view))
		if err != nil {
			return nil, err
		}
		queries = append(queries, inventoryQueriesForViews([]string{view}, intervals)...)
	}
	return queries, nil
}

func (s *inventoryNetworkSelector) configured() bool {
	return len(s.anyView) > 0 || len(s.byView) > 0
}

func (s *inventoryNetworkSelector) count() int {
	count := len(s.anyView)
	for _, networks := range s.byView {
		for network := range networks {
			if _, exists := s.anyView[network]; !exists {
				count++
			}
		}
	}
	return count
}

func (s *inventoryNetworkSelector) matches(network string, view string) bool {
	if _, exists := s.anyView[network]; exists {
		return true
	}
	_, exists := s.byView[view][network]
	return exists
}

func (s *inventoryNetworkSelector) allNetworks() []string {
	values := make([]string, 0, s.count())
	for network := range s.anyView {
		values = append(values, network)
	}
	for _, networks := range s.byView {
		for network := range networks {
			values = append(values, network)
		}
	}
	return values
}

func (s *inventoryNetworkSelector) networksForView(view string) []string {
	values := make([]string, 0, len(s.anyView)+len(s.byView[view]))
	for network := range s.anyView {
		values = append(values, network)
	}
	for network := range s.byView[view] {
		values = append(values, network)
	}
	return values
}

func mergedInventoryIntervals(values []string) ([]inventoryInterval, error) {
	intervals := make([]inventoryInterval, 0, len(values))
	for _, value := range values {
		prefix, err := parseIPv4Prefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid IPv4 inventory CIDR %q: %w", value, err)
		}
		start := ipv4Uint32(prefix.Addr())
		hostBits := 32 - prefix.Bits()
		var end uint32 = ^uint32(0)
		if hostBits < 32 {
			end = start | (uint32(1)<<hostBits - 1)
		}
		intervals = append(intervals, inventoryInterval{start: start, end: end})
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start != intervals[j].start {
			return intervals[i].start < intervals[j].start
		}
		return intervals[i].end < intervals[j].end
	})
	merged := make([]inventoryInterval, 0, len(intervals))
	for _, current := range intervals {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}
		last := &merged[len(merged)-1]
		adjacent := last.end != ^uint32(0) && current.start == last.end+1
		if current.start <= last.end || adjacent {
			if current.end > last.end {
				last.end = current.end
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged, nil
}

func inventoryQueriesForViews(views []string, intervals []inventoryInterval) []inventoryQuery {
	queries := make([]inventoryQuery, 0, len(views)*len(intervals))
	for _, view := range views {
		for _, interval := range intervals {
			queries = append(queries, inventoryQuery{view: view, interval: interval, bounded: true})
		}
	}
	return queries
}

func uniqueBoundedIntervals(queries []inventoryQuery) int {
	unique := make(map[inventoryInterval]struct{})
	for _, query := range queries {
		if query.bounded {
			unique[query.interval] = struct{}{}
		}
	}
	return len(unique)
}

func setInventoryBounds(params url.Values, interval inventoryInterval) {
	// In WAPI query syntax, the key suffixes produce inclusive >= and <= searches.
	// Both operands must remain within a managed network or the appliance rejects the query.
	params.Set("ip_address>", ipv4Addr(interval.start).String())
	params.Set("ip_address<", ipv4Addr(interval.end).String())
}

func parseIPv4Prefix(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return netip.Prefix{}, err
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("not an IPv4 prefix")
	}
	return prefix.Masked(), nil
}

func ipv4Uint32(address netip.Addr) uint32 {
	bytes := address.As4()
	return uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
}

func ipv4Addr(value uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
