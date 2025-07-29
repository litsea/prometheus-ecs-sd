package main

import (
	"os"

	"github.com/spf13/pflag"

	"github.com/litsea/prometheus-ecs-sd/internal/client"
	"github.com/litsea/prometheus-ecs-sd/internal/discovery"
	"github.com/litsea/prometheus-ecs-sd/internal/log"
)

var (
	addr        string
	logLevel    string
	ecsClusters []string
)

func main() {
	pflag.StringVar(&addr, "http.addr", ":10101", "HTTP listen addr")
	pflag.StringVar(&logLevel, "log.level", "info", "Set logging verbosity (debug, info, warn, error)")
	pflag.StringSliceVar(&ecsClusters, "ecs.clusters", nil, "Set ECS clusters, separated by commas")
	pflag.Parse()

	l := log.New(logLevel)

	disc, err := discovery.New(
		discovery.WithHTTPAddr(addr),
		discovery.WithLogger(l),
		discovery.WithAWSECSClient(
			client.NewECSCache(l, client.NewDefaultECS()),
		),
		discovery.WithECSClusters(ecsClusters),
	)
	if err != nil {
		l.Error("create discovery failed", "err", err)
		os.Exit(1)
	}

	err = disc.Run()
	if err != nil {
		l.Error(err.Error())
	}
}
