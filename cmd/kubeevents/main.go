package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/kubeevents/kubeevents/internal/api"
	"github.com/kubeevents/kubeevents/internal/config"
	"github.com/kubeevents/kubeevents/internal/notifier"
	"github.com/kubeevents/kubeevents/internal/store"
	"github.com/kubeevents/kubeevents/internal/watcher"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		klog.ErrorS(err, "invalid configuration")
		os.Exit(1)
	}

	kubeCfg, err := buildKubeConfig()
	if err != nil {
		klog.ErrorS(err, "failed to build kubernetes config")
		os.Exit(1)
	}

	client, err := kubernetes.NewForConfig(kubeCfg)
	if err != nil {
		klog.ErrorS(err, "failed to create kubernetes client")
		os.Exit(1)
	}

	tg := notifier.NewTelegram(cfg)
	eventStore := store.New(cfg.UIEventCapacity)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	apiServer := api.New(eventStore, cfg.ClusterName, cfg.Namespace)
	srv := &http.Server{Addr: cfg.HealthAddr, Handler: apiServer.Handler()}
	go func() {
		klog.InfoS("web UI and API listening", "addr", cfg.HealthAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.ErrorS(err, "http server failed")
			cancel()
		}
	}()

	if cfg.SendStartupMessage {
		startupCtx, startupCancel := context.WithTimeout(ctx, 15*time.Second)
		if err := tg.SendStartup(startupCtx); err != nil {
			klog.ErrorS(err, "failed to send startup telegram message (continuing)")
		} else {
			klog.InfoS("startup telegram message sent")
		}
		startupCancel()
	}

	ns := cfg.Namespace
	if ns == "" {
		ns = "all namespaces"
	}
	klog.InfoS("starting kubeevents watcher",
		"cluster", cfg.ClusterName,
		"namespace", ns,
		"minSeverity", cfg.MinSeverity,
		"skipNoisyNormals", cfg.SkipNoisyNormals,
		"uiCapacity", cfg.UIEventCapacity,
	)

	ew := watcher.New(client, cfg, tg, eventStore)
	if err := ew.Run(ctx); err != nil && err != context.Canceled {
		klog.ErrorS(err, "watcher stopped with error")
		os.Exit(1)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	klog.InfoS("shutdown complete")
}

func buildKubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("in-cluster and kubeconfig both failed: %w", err)
	}
	return cfg, nil
}
