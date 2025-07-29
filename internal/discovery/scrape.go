package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

var (
	labelEcsPrefix           = model.MetaLabelPrefix + "ecs_"
	labelEcsServiceTagPrefix = labelEcsPrefix + "service_tag_"
	labelClusterName         = model.LabelName(labelEcsPrefix + "cluster_name")
	labelServiceName         = model.LabelName(labelEcsPrefix + "service_name")
	labelTaskID              = model.LabelName(labelEcsPrefix + "task_id")
)

func (d *Discovery) writeScrapeConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	scrapConfig, err := d.buildScrapeConfig(context.Background())
	if err != nil {
		d.logger.Error("build scrape config failed", "err", err)
	}

	jsonBytes, err := json.MarshalIndent(scrapConfig, "", "  ")
	if err != nil {
		d.logger.Error("encoding scrape config failed", "err", err)
		_, _ = w.Write([]byte("[]"))
		return
	}

	_, _ = w.Write(jsonBytes)
}

func (d *Discovery) buildScrapeConfig(ctx context.Context) ([]*targetgroup.Group, error) {
	d.logger.Info("scraping ECS targets", "clusters", d.clusters)

	clusters, err := d.client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
		Clusters: d.clusters,
	})
	if err != nil {
		return nil, fmt.Errorf("describe clusters failed: clusters=%v, %w", d.clusters, err)
	}

	tgs := make([]*targetgroup.Group, 0)

	if len(clusters.Clusters) == 0 {
		return tgs, fmt.Errorf("no valid clusters found: clusters=%v", d.clusters)
	}

	for _, cluster := range clusters.Clusters {
		clusterName := *cluster.ClusterName
		clusterArn := cluster.ClusterArn

		d.logger.Info("listing ECS services", "cluster", clusterName)

		services, err := d.client.ListServices(ctx, &ecs.ListServicesInput{
			Cluster: clusterArn,
		})
		if err != nil {
			return nil, fmt.Errorf("listing ECS services failed: cluster=%s, %w",
				clusterName, err)
		}

		for _, service := range services.ServiceArns {
			serviceName := service[strings.LastIndex(service, "/")+1:]

			d.logger.Info("listing ECS tasks", "cluster", clusterName, "service", serviceName)

			taskList, err := d.client.ListTasks(ctx, &ecs.ListTasksInput{
				Cluster:     clusterArn,
				ServiceName: aws.String(service),
			})
			if err != nil {
				return nil, fmt.Errorf("listing ECS tasks failed: cluster=%s, service=%s, %w",
					clusterName, serviceName, err)
			}

			tasks, err := d.client.DescribeTasks(ctx, &ecs.DescribeTasksInput{
				Cluster: clusterArn,
				Tasks:   taskList.TaskArns,
			})
			if err != nil {
				return nil, fmt.Errorf("describing tasks failed: cluster=%s, service=%s, %w",
					clusterName, serviceName, err)
			}

			tags, err := d.client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
				ResourceArn: aws.String(service),
			})
			if err != nil {
				return nil, fmt.Errorf("listing ECS tags failed: cluster=%s, service=%s, %w",
					clusterName, serviceName, err)
			}

			metricsPort := getTag(tags.Tags, "metrics_port")
			if metricsPort == "" {
				metricsPort = "80"
			}

			for _, task := range tasks.Tasks {
				if len(task.Containers) == 0 || len(task.Containers[0].NetworkInterfaces) == 0 {
					d.logger.Debug("ECS task has no network interfaces",
						"cluster", clusterName, "service", serviceName, "task", task)
					continue
				}

				if task.HealthStatus != types.HealthStatusHealthy {
					d.logger.Debug("ECS task is unhealthy",
						"cluster", clusterName, "service", serviceName, "task", task)
					continue
				}

				ip := task.Containers[0].NetworkInterfaces[0].PrivateIpv4Address
				if ip == nil {
					continue
				}

				taskArn := *task.TaskArn
				taskID := taskArn[strings.LastIndex(taskArn, "/")+1:]
				tg := &targetgroup.Group{
					Source: clusterName + "/" + serviceName,
					Labels: tagsToLabelSet(tags.Tags).Merge(model.LabelSet{
						labelClusterName: model.LabelValue(clusterName),
						labelServiceName: model.LabelValue(serviceName),
						labelTaskID:      model.LabelValue(taskID),
					}),
					Targets: []model.LabelSet{
						{
							model.AddressLabel: model.LabelValue(*ip + ":" + metricsPort),
						},
					},
				}

				tgs = append(tgs, tg)
			}
		}
	}

	d.logger.Info("scraped ECS targets", "clusters", d.clusters, "targets", len(tgs))
	return tgs, nil
}

func getTag(tags []types.Tag, key string) string {
	for _, tag := range tags {
		if *tag.Key == key {
			return *tag.Value
		}
	}

	return ""
}

func tagsToLabelSet(tags []types.Tag) model.LabelSet {
	labels := model.LabelSet{}
	valid := regexp.MustCompile("[^a-zA-Z0-9_]")
	separator := regexp.MustCompile("([a-z])([A-Z])")
	for _, tag := range tags {
		// convert tag key to valid label name
		key := valid.ReplaceAllString(*tag.Key, "_")
		// add _ between big letters
		key = separator.ReplaceAllString(key, "${1}_${2}")
		// convert to lower case
		key = strings.ToLower(key)
		labels[model.LabelName(labelEcsServiceTagPrefix+key)] = model.LabelValue(*tag.Value)
	}
	return labels
}
