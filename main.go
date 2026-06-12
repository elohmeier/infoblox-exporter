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
		urlStr             string
		labelsStr          string
		disabledModulesStr string
		caFile             string
		networkViewsStr    string
		dnsViewsStr        string
		networksStr        string
		zonesStr           string
		upgradeTypesStr    string
		bindPort           int
		pageSize           int
		timeout            time.Duration
		ignoreCert         bool
		showVersion        bool
		debug              bool
	)

	flags := flag.NewFlagSet(app, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&urlStr, "url", "", "Infoblox WAPI URL (e.g., https://gm.example.com/wapi/v2.13.7)")
	flags.StringVar(&labelsStr, "labels", "", "Custom labels in key=value format, comma-separated (e.g., env=prod,dc=de)")
	flags.StringVar(&disabledModulesStr, "disabled-modules", "", "Comma-separated list of collectors to disable")
	flags.IntVar(&bindPort, "bind-port", 9717, "Port to bind the exporter endpoint to")
	flags.IntVar(&pageSize, "page-size", 0, "WAPI page size (default: 1000)")
	flags.DurationVar(&timeout, "timeout", 0, "WAPI request timeout (default: 30s)")
	flags.BoolVar(&ignoreCert, "ignore-cert", false, "Disable TLS certificate verification")
	flags.StringVar(&caFile, "ca-file", "", "Path to a custom CA certificate bundle")
	flags.StringVar(&networkViewsStr, "network-views", "", "Comma-separated network views to query (default: all)")
	flags.StringVar(&dnsViewsStr, "dns-views", "", "Comma-separated DNS views to query (default: default)")
	flags.StringVar(&networksStr, "networks", "", "Comma-separated CIDRs to scope network, range, IPv4, DHCP, and IPAM collectors")
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
	cfg.Zones = chooseCSV(zonesStr, config.GetZones(), cfg.Zones)
	cfg.UpgradeStatusTypes = chooseCSV(upgradeTypesStr, config.GetUpgradeStatusTypes(), cfg.UpgradeStatusTypes)

	var err error
	cfg.PageSize, err = chooseInt(pageSize, config.GetPageSize(), cfg.PageSize, "page-size")
	if err != nil {
		logger.Error("invalid page size", "err", err)
		return 1
	}
	cfg.Timeout, err = chooseDuration(timeout, config.GetTimeout(), cfg.Timeout, "timeout")
	if err != nil {
		logger.Error("invalid timeout", "err", err)
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
	registerer.MustRegister(wapiMetrics.Collectors()...)
	registerer.MustRegister(newExporter(cfg, client, logger))

	listenAddr := ":" + strconv.Itoa(bindPort)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           newMux(registry),
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
	if err := shutdown(server, ctx); err != nil {
		logger.Error("server shutdown failed", "err", err)
		return 1
	}
	return 0
}

func newMux(registry *prometheus.Registry) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
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
