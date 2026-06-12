package model

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

type Uint64 struct {
	Value uint64
	Valid bool
}

func (u *Uint64) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		*u = Uint64{}
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*u = Uint64{}
			return nil
		}
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		*u = Uint64{Value: v, Valid: true}
		return nil
	}

	var v uint64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*u = Uint64{Value: v, Valid: true}
	return nil
}

type Timestamp struct {
	Value int64
	Valid bool
}

func (t *Timestamp) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		*t = Timestamp{}
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*t = Timestamp{}
			return nil
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*t = Timestamp{Value: v, Valid: true}
		return nil
	}

	var v int64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*t = Timestamp{Value: v, Valid: true}
	return nil
}

type Network struct {
	Ref                   string    `json:"_ref"`
	Network               string    `json:"network"`
	NetworkView           string    `json:"network_view"`
	Comment               string    `json:"comment"`
	Disable               bool      `json:"disable"`
	Utilization           Uint64    `json:"utilization"`
	UtilizationUpdate     Timestamp `json:"utilization_update"`
	DHCPUtilization       Uint64    `json:"dhcp_utilization"`
	DHCPUtilizationStatus string    `json:"dhcp_utilization_status"`
}

type Range struct {
	Ref                   string `json:"_ref"`
	Network               string `json:"network"`
	NetworkView           string `json:"network_view"`
	StartAddr             string `json:"start_addr"`
	EndAddr               string `json:"end_addr"`
	Name                  string `json:"name"`
	Comment               string `json:"comment"`
	Disable               bool   `json:"disable"`
	ServerAssociationType string `json:"server_association_type"`
	FailoverAssociation   string `json:"failover_association"`
	DHCPUtilization       Uint64 `json:"dhcp_utilization"`
	DHCPUtilizationStatus string `json:"dhcp_utilization_status"`
	DynamicHosts          Uint64 `json:"dynamic_hosts"`
}

type IPv4Address struct {
	Ref         string   `json:"_ref"`
	IPAddress   string   `json:"ip_address"`
	Network     string   `json:"network"`
	NetworkView string   `json:"network_view"`
	Status      string   `json:"status"`
	LeaseState  string   `json:"lease_state"`
	Types       []string `json:"types"`
	Usage       []string `json:"usage"`
	IsConflict  bool     `json:"is_conflict"`
}

type Member struct {
	Ref                      string                   `json:"_ref"`
	HostName                 string                   `json:"host_name"`
	Platform                 string                   `json:"platform"`
	ServiceTypeConfiguration string                   `json:"service_type_configuration"`
	ServiceStatus            []map[string]interface{} `json:"service_status"`
}

type RestartServiceStatus struct {
	Member          string `json:"member"`
	DHCPStatus      string `json:"dhcp_status"`
	DNSStatus       string `json:"dns_status"`
	ReportingStatus string `json:"reporting_status"`
}

type ServiceRestartStatus struct {
	Ref            string `json:"_ref"`
	Parent         string `json:"parent"`
	Grouped        string `json:"grouped"`
	Failures       Uint64 `json:"failures"`
	Finished       Uint64 `json:"finished"`
	NeededRestart  Uint64 `json:"needed_restart"`
	NoRestart      Uint64 `json:"no_restart"`
	Pending        Uint64 `json:"pending"`
	PendingRestart Uint64 `json:"pending_restart"`
	Processing     Uint64 `json:"processing"`
	Restarting     Uint64 `json:"restarting"`
	Success        Uint64 `json:"success"`
	Timeouts       Uint64 `json:"timeouts"`
}

type ServiceRestartRequest struct {
	Ref             string    `json:"_ref"`
	Error           string    `json:"error"`
	Forced          bool      `json:"forced"`
	Group           string    `json:"group"`
	LastUpdatedTime Timestamp `json:"last_updated_time"`
	Member          string    `json:"member"`
	Needed          string    `json:"needed"`
	Order           Uint64    `json:"order"`
	Result          string    `json:"result"`
	Service         string    `json:"service"`
	State           string    `json:"state"`
}

type CapacityReport struct {
	Ref          string                   `json:"_ref"`
	Name         string                   `json:"name"`
	HardwareType string                   `json:"hardware_type"`
	Role         string                   `json:"role"`
	MaxCapacity  Uint64                   `json:"max_capacity"`
	PercentUsed  Uint64                   `json:"percent_used"`
	TotalObjects Uint64                   `json:"total_objects"`
	ObjectCounts []map[string]interface{} `json:"object_counts"`
}

type License struct {
	Ref              string    `json:"_ref"`
	Scope            string    `json:"-"`
	HWID             string    `json:"hwid"`
	Type             string    `json:"type"`
	Kind             string    `json:"kind"`
	Limit            string    `json:"limit"`
	LimitContext     string    `json:"limit_context"`
	ExpirationStatus string    `json:"expiration_status"`
	ExpiryDate       Timestamp `json:"expiry_date"`
}

type UpgradeStatus struct {
	Ref                   string    `json:"_ref"`
	Type                  string    `json:"type"`
	Member                string    `json:"member"`
	UpgradeGroup          string    `json:"upgrade_group"`
	CurrentVersion        string    `json:"current_version"`
	DistributionVersion   string    `json:"distribution_version"`
	UploadVersion         string    `json:"upload_version"`
	ElementStatus         string    `json:"element_status"`
	GridState             string    `json:"grid_state"`
	GroupState            string    `json:"group_state"`
	HAStatus              string    `json:"ha_status"`
	Message               string    `json:"message"`
	PNodeRole             string    `json:"pnode_role"`
	Reverted              bool      `json:"reverted"`
	StatusValue           string    `json:"status_value"`
	StatusValueUpdateTime Timestamp `json:"status_value_update_time"`
	StepsCompleted        Uint64    `json:"steps_completed"`
	StepsTotal            Uint64    `json:"steps_total"`
	UpgradeState          string    `json:"upgrade_state"`
	UpgradeTestStatus     string    `json:"upgrade_test_status"`
}

type DHCPStatistics struct {
	Ref                   string `json:"_ref"`
	DHCPUtilization       Uint64 `json:"dhcp_utilization"`
	DHCPUtilizationStatus string `json:"dhcp_utilization_status"`
	DynamicHosts          Uint64 `json:"dynamic_hosts"`
	StaticHosts           Uint64 `json:"static_hosts"`
	TotalHosts            Uint64 `json:"total_hosts"`
}

type IPAMStatistics struct {
	Ref               string    `json:"_ref"`
	CIDR              Uint64    `json:"cidr"`
	Network           string    `json:"network"`
	NetworkView       string    `json:"network_view"`
	ConflictCount     Uint64    `json:"conflict_count"`
	UnmanagedCount    Uint64    `json:"unmanaged_count"`
	Utilization       Uint64    `json:"utilization"`
	UtilizationUpdate Timestamp `json:"utilization_update"`
}

type DHCPFailover struct {
	Ref                 string `json:"_ref"`
	Name                string `json:"name"`
	AssociationType     string `json:"association_type"`
	Comment             string `json:"comment"`
	Primary             string `json:"primary"`
	Secondary           string `json:"secondary"`
	PrimaryServerType   string `json:"primary_server_type"`
	SecondaryServerType string `json:"secondary_server_type"`
	LoadBalanceSplit    Uint64 `json:"load_balance_split"`
	MaxClientLeadTime   Uint64 `json:"max_client_lead_time"`
	MaxResponseDelay    Uint64 `json:"max_response_delay"`
	MaxUnackedUpdates   Uint64 `json:"max_unacked_updates"`
}

type AllRecord struct {
	Ref         string `json:"_ref"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	View        string `json:"view"`
	Zone        string `json:"zone"`
	Disable     bool   `json:"disable"`
	Reclaimable bool   `json:"reclaimable"`
	TTL         Uint64 `json:"ttl"`
}

type Zone struct {
	Ref     string `json:"_ref"`
	FQDN    string `json:"fqdn"`
	View    string `json:"view"`
	Comment string `json:"comment"`
	Disable bool   `json:"disable"`
}

type DTCObject struct {
	Ref          string `json:"_ref"`
	Name         string `json:"name"`
	Comment      string `json:"comment"`
	AbstractType string `json:"abstract_type"`
	DisplayType  string `json:"display_type"`
	Status       string `json:"status"`
}

type ThreatProtectionStatistics struct {
	Member    string                   `json:"member"`
	StatInfos []map[string]interface{} `json:"stat_infos"`
}
