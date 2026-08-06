package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/elohmeier/infoblox-exporter/internal/collector"
	"github.com/elohmeier/infoblox-exporter/internal/config"
	"github.com/elohmeier/infoblox-exporter/internal/wapi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	app     = "Infoblox-Exporter"
	version = "dev"
	build   = "none"
)

var (
	exit              = os.Exit
	listenAndServe    = (*http.Server).ListenAndServe
	shutdownServer    = (*http.Server).Shutdown
	signalNotify      = signal.Notify
	signalStop        = signal.Stop
	newWAPIClient     = wapi.NewClient
	newWAPIMetrics    = wapi.NewMetrics
	newExporter       = collector.New
	defaultRegisterer prometheus.Registerer
)

func main() {
	exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	var (
		urlStr                 string
		labelsStr              string
		disabledModulesStr     string
		caFile                 string
		networkViewsStr        string
		dnsViewsStr            string
		networksStr            string
		inventoryNetworksStr   string
		inventoryScanRangesStr string
		inventoryNameRegex     string
		inventoryNetworkEA     string
		zonesStr               string
		upgradeTypesStr        string
		bindPort               int
		pageSize               int
		inventoryPageSize      int
		inventoryMaxAddresses  int
		timeout                time.Duration
		refreshInterval        time.Duration
		refreshTimeout         time.Duration
		maxStale               time.Duration
		inventoryTimeout       time.Duration
		ignoreCert             bool
		showVersion            bool
		debug                  bool
	)

	flags := flag.NewFlagSet(app, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&urlStr, "url", "", "Infoblox WAPI URL (e.g., https://gm.example.com/wapi/v2.13.7)")
	flags.StringVar(&labelsStr, "labels", "", "Custom labels in key=value format, comma-separated (e.g., env=prod,dc=de)")
	flags.StringVar(&disabledModulesStr, "disabled-modules", "", "Comma-separated list of collectors to disable")
	flags.IntVar(&bindPort, "bind-port", 9717, "Port to bind the exporter endpoint to")
	flags.IntVar(&pageSize, "page-size", 0, "WAPI page size (default: 1000)")
	flags.DurationVar(&timeout, "timeout", 0, "WAPI request timeout (default: 30s)")
	flags.DurationVar(&refreshInterval, "refresh-interval", 0, "Background cache refresh interval (default: 5m)")
	flags.DurationVar(&refreshTimeout, "refresh-timeout", 0, "Background cache refresh timeout (default: 2m)")
	flags.DurationVar(&maxStale, "max-stale", 0, "Maximum cache age before readiness fails (default: 15m)")
	flags.BoolVar(&ignoreCert, "ignore-cert", false, "Disable TLS certificate verification")
	flags.StringVar(&caFile, "ca-file", "", "Path to a custom CA certificate bundle")
	flags.StringVar(&networkViewsStr, "network-views", "", "Comma-separated network views to query (default: all)")
	flags.StringVar(&dnsViewsStr, "dns-views", "", "Comma-separated DNS views to query (default: all)")
	flags.StringVar(&networksStr, "networks", "", "Comma-separated CIDRs to scope network, range, DHCP, and IPAM collectors")
	flags.StringVar(&inventoryNetworksStr, "ipv4-inventory-networks", "", "Comma-separated network CIDRs selected for IPv4 inventory")
	flags.StringVar(&inventoryScanRangesStr, "ipv4-inventory-scan-ranges", "", "Comma-separated CIDRs used as IPv4 inventory query intervals")
	flags.StringVar(&inventoryNameRegex, "ipv4-inventory-name-regex", "", "WAPI regular expression used to select IPv4 inventory names")
	flags.StringVar(&inventoryNetworkEA, "ipv4-inventory-network-ea", "", "Network extensible attribute selector in name=value form")
	flags.IntVar(&inventoryPageSize, "ipv4-inventory-page-size", 0, "WAPI page size for IPv4 inventory (default: 2000)")
	flags.IntVar(&inventoryMaxAddresses, "ipv4-inventory-max-addresses", 0, "Maximum occupied IPv4 addresses retained per refresh (default: 100000)")
	flags.DurationVar(&inventoryTimeout, "ipv4-inventory-timeout", 0, "Overall IPv4 inventory collector timeout (default: 5m)")
	flags.StringVar(&zonesStr, "zones", "", "Comma-separated DNS zones to scope allrecords and zones collectors")
	flags.StringVar(&upgradeTypesStr, "upgrade-status-types", "", "Comma-separated upgrade status types")
	flags.BoolVar(&showVersion, "version", false, "Display application version")
	flags.BoolVar(&debug, "debug", false, "Enable debug logging")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if showVersion {
		_, _ = fmt.Fprintf(stdout, "%s v%s build %s\n", app, version, build)
		return 0
	}

	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	})).With("app", app, "version", "v"+version, "build", build)

	if urlStr == "" {
		urlStr = config.GetURL()
	}
	if urlStr == "" {
		logger.Error("URL is required (use -url flag or INFOBLOX_URL env var)")
		flags.Usage()
		return 1
	}

	username, password := config.GetCredentials()
	if username == "" || password == "" {
		logger.Error("credentials are required via INFOBLOX_USERNAME and INFOBLOX_PASSWORD")
		return 1
	}

	cfg := config.Default()
	cfg.Labels = config.ParseLabels(config.GetLabels())
	for key, value := range config.ParseLabels(labelsStr) {
		cfg.Labels[key] = value
	}
	cfg.DisabledModules = append(config.ParseDisabledModules(config.GetDisabledModules()), config.ParseDisabledModules(disabledModulesStr)...)
	cfg.NetworkViews = chooseCSV(networkViewsStr, config.GetNetworkViews(), cfg.NetworkViews)
	cfg.DNSViews = chooseCSV(dnsViewsStr, config.GetDNSViews(), cfg.DNSViews)
	cfg.Networks = chooseCSV(networksStr, config.GetNetworks(), cfg.Networks)
	cfg.IPv4InventoryNetworks = chooseCSV(inventoryNetworksStr, config.GetIPv4InventoryNetworks(), cfg.IPv4InventoryNetworks)
	cfg.IPv4InventoryScanRanges = chooseCSV(inventoryScanRangesStr, config.GetIPv4InventoryScanRanges(), cfg.IPv4InventoryScanRanges)
	cfg.IPv4InventoryNameRegex = chooseString(inventoryNameRegex, config.GetIPv4InventoryNameRegex(), cfg.IPv4InventoryNameRegex)
	cfg.IPv4InventoryNetworkEA = chooseString(inventoryNetworkEA, config.GetIPv4InventoryNetworkEA(), cfg.IPv4InventoryNetworkEA)
	cfg.Zones = chooseCSV(zonesStr, config.GetZones(), cfg.Zones)
	cfg.UpgradeStatusTypes = chooseCSV(upgradeTypesStr, config.GetUpgradeStatusTypes(), cfg.UpgradeStatusTypes)

	var err error
	cfg.PageSize, err = chooseInt(pageSize, config.GetPageSize(), cfg.PageSize, "page-size")
	if err != nil {
		logger.Error("invalid page size", "err", err)
		return 1
	}
	cfg.IPv4InventoryPageSize, err = chooseInt(inventoryPageSize, config.GetIPv4InventoryPageSize(), cfg.IPv4InventoryPageSize, "ipv4-inventory-page-size")
	if err != nil {
		logger.Error("invalid IPv4 inventory page size", "err", err)
		return 1
	}
	cfg.IPv4InventoryMaxAddresses, err = chooseInt(inventoryMaxAddresses, config.GetIPv4InventoryMaxAddresses(), cfg.IPv4InventoryMaxAddresses, "ipv4-inventory-max-addresses")
	if err != nil {
		logger.Error("invalid IPv4 inventory maximum address count", "err", err)
		return 1
	}
	cfg.Timeout, err = chooseDuration(timeout, config.GetTimeout(), cfg.Timeout, "timeout")
	if err != nil {
		logger.Error("invalid timeout", "err", err)
		return 1
	}
	cfg.RefreshInterval, err = chooseDuration(refreshInterval, config.GetRefreshInterval(), cfg.RefreshInterval, "refresh-interval")
	if err != nil {
		logger.Error("invalid refresh interval", "err", err)
		return 1
	}
	cfg.RefreshTimeout, err = chooseDuration(refreshTimeout, config.GetRefreshTimeout(), cfg.RefreshTimeout, "refresh-timeout")
	if err != nil {
		logger.Error("invalid refresh timeout", "err", err)
		return 1
	}
	cfg.MaxStale, err = chooseDuration(maxStale, config.GetMaxStale(), cfg.MaxStale, "max-stale")
	if err != nil {
		logger.Error("invalid max stale", "err", err)
		return 1
	}
	cfg.IPv4InventoryTimeout, err = chooseDuration(inventoryTimeout, config.GetIPv4InventoryTimeout(), cfg.IPv4InventoryTimeout, "ipv4-inventory-timeout")
	if err != nil {
		logger.Error("invalid IPv4 inventory timeout", "err", err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "err", err)
		return 1
	}

	if caFile == "" {
		caFile = config.GetCAFile()
	}
	ignoreCert = ignoreCert || config.GetIgnoreCert()

	if ignoreCert {
		logger.Info("TLS certificate verification disabled")
	}
	if caFile != "" {
		logger.Info("using custom CA file", "path", caFile)
	}

	logger.Info(
		"starting exporter",
		"url", urlStr,
		"labels", len(cfg.Labels),
		"disabled_modules", len(cfg.DisabledModules),
		"networks", len(cfg.Networks),
		"ipv4_inventory_networks", len(cfg.IPv4InventoryNetworks),
		"ipv4_inventory_scan_ranges", len(cfg.IPv4InventoryScanRanges),
		"ipv4_inventory_name_regex", cfg.IPv4InventoryNameRegex != "",
		"ipv4_inventory_network_ea", cfg.IPv4InventoryNetworkEA != "",
		"zones", len(cfg.Zones),
	)

	wapiMetrics := newWAPIMetrics("infoblox")
	client, err := newWAPIClient(wapi.Config{
		BaseURL:            urlStr,
		Username:           username,
		Password:           password,
		Timeout:            cfg.Timeout,
		PageSize:           cfg.PageSize,
		InsecureSkipVerify: ignoreCert,
		CAFile:             caFile,
		UserAgent:          fmt.Sprintf("infoblox-exporter/%s", version),
		Metrics:            wapiMetrics,
	})
	if err != nil {
		logger.Error("failed to create WAPI client", "err", err)
		return 1
	}

	registry := prometheus.NewRegistry()
	registerer := prometheus.Registerer(registry)
	if defaultRegisterer != nil {
		registerer = defaultRegisterer
	}
	if len(cfg.Labels) > 0 {
		registerer = prometheus.WrapRegistererWith(cfg.Labels, registerer)
	}
	exporter := newExporter(cfg, client, logger)
	registerer.MustRegister(wapiMetrics.Collectors()...)
	registerer.MustRegister(exporter)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	exporter.Start(appCtx)
	defer exporter.Stop()

	listenAddr := ":" + strconv.Itoa(bindPort)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           newMux(registry, exporter),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serve := listenAndServe
	shutdown := shutdownServer
	notify := signalNotify
	stop := signalStop

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", listenAddr)
		errCh <- serve(server)
	}()

	sigCh := make(chan os.Signal, 1)
	notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer stop(sigCh)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			return 1
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	appCancel()
	exporter.Stop()
	if err := shutdown(server, ctx); err != nil {
		logger.Error("server shutdown failed", "err", err)
		return 1
	}
	return 0
}

func newMux(registry *prometheus.Registry, exporter ...*collector.Exporter) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	if len(exporter) > 0 && exporter[0] != nil {
		mux.HandleFunc("/readyz", exporter[0].ReadyHandler)
		mux.HandleFunc("/debug/cache", exporter[0].DebugCacheHandler)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(app + " - /metrics for Prometheus metrics"))
	})
	return mux
}

func chooseCSV(flagValue string, envValue string, defaultValue []string) []string {
	switch {
	case flagValue != "":
		return config.ParseCSV(flagValue)
	case envValue != "":
		return config.ParseCSV(envValue)
	default:
		return append([]string(nil), defaultValue...)
	}
}

func chooseString(flagValue string, envValue string, defaultValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if envValue != "" {
		return envValue
	}
	return defaultValue
}

func chooseInt(flagValue int, envValue string, defaultValue int, name string) (int, error) {
	value := defaultValue
	if envValue != "" {
		parsed, err := strconv.Atoi(envValue)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer: %w", name, err)
		}
		value = parsed
	}
	if flagValue != 0 {
		value = flagValue
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return value, nil
}

func chooseDuration(flagValue time.Duration, envValue string, defaultValue time.Duration, name string) (time.Duration, error) {
	value := defaultValue
	if envValue != "" {
		parsed, err := time.ParseDuration(envValue)
		if err != nil {
			return 0, fmt.Errorf("%s must be a duration: %w", name, err)
		}
		value = parsed
	}
	if flagValue != 0 {
		value = flagValue
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return value, nil
}
