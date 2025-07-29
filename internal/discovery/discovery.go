package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/common/model"

	"github.com/litsea/prometheus-ecs-sd/internal/client"
	"github.com/litsea/prometheus-ecs-sd/internal/log"
)

var (
	labelEcsPrefix           = model.MetaLabelPrefix + "ecs_"
	labelEcsServiceTagPrefix = labelEcsPrefix + "service_tag_"
	labelClusterName         = model.LabelName(labelEcsPrefix + "cluster_name")
	labelServiceName         = model.LabelName(labelEcsPrefix + "service_name")
)

type Option func(*Discovery) error

type Discovery struct {
	client   client.ECS
	addr     string
	logger   log.Logger
	clusters []string
}

func WithAWSECSClient(client client.ECS) Option {
	return func(d *Discovery) error {
		if client == nil {
			return errors.New("aws ecs client must not be nil")
		}

		d.client = client
		return nil
	}
}

func WithHTTPAddr(addr string) Option {
	return func(d *Discovery) error {
		d.addr = addr
		return nil
	}
}

func WithECSClusters(cs []string) Option {
	return func(d *Discovery) error {
		if len(cs) == 0 {
			return errors.New("ecs clusters must not be empty")
		}
		d.clusters = cs
		return nil
	}
}

func WithLogger(logger log.Logger) Option {
	return func(d *Discovery) error {
		if logger == nil {
			return errors.New("logger must not be nil")
		}
		d.logger = logger
		return nil
	}
}

func New(opts ...Option) (*Discovery, error) {
	d := &Discovery{
		logger: log.NewNopLogger(),
	}

	for _, opt := range opts {
		if err := opt(d); err != nil {
			return nil, err
		}
	}

	if d.client == nil {
		d.client = client.NewDefaultECS()
	}

	return d, nil
}

func (d *Discovery) Run() error {
	// kill -INT <pid>
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	d.logger.Info("service discovery starting in HTTP-based mode", "clusters", d.clusters)
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/prometheus-targets", d.writeScrapeConfig)

	server := &http.Server{
		Addr:           d.addr,
		Handler:        serveMux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	server.SetKeepAlivesEnabled(true)

	errCh := make(chan error, 1)

	go func(server *http.Server) {
		d.logger.Info("starting service discovery HTTP server", "addr", d.addr)
		errCh <- server.ListenAndServe()
	}(server)

	select {
	case <-sigs:
		d.logger.Info("service shutting down gracefully")

		sdCtx, sdCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer sdCancel()

		done := make(chan error, 1)
		go func() {
			done <- server.Shutdown(sdCtx)
		}()

		select {
		case <-sdCtx.Done():
			return fmt.Errorf("service shutdown timeout: %w", sdCtx.Err())
		case err := <-done:
			if err != nil {
				return fmt.Errorf("service forced to shutdown: %w", err)
			}
			d.logger.Info("service gracefully stopped")
			return nil
		}
	case err := <-errCh:
		return fmt.Errorf("service start failed: %w", err)
	}
}
