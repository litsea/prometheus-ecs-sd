package client

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/litsea/prometheus-ecs-sd/internal/cache"
	"github.com/litsea/prometheus-ecs-sd/internal/log"
)

type ECS interface {
	DescribeClusters(ctx context.Context, params *ecs.DescribeClustersInput, optFns ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error)
	ListServices(ctx context.Context, params *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error)
	ListTasks(ctx context.Context, params *ecs.ListTasksInput, optFns ...func(*ecs.Options)) (*ecs.ListTasksOutput, error)
	DescribeTasks(ctx context.Context, params *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
	ListTagsForResource(ctx context.Context, params *ecs.ListTagsForResourceInput, optFns ...func(*ecs.Options)) (*ecs.ListTagsForResourceOutput, error)
}

type ECSCache struct {
	client ECS
	logger log.Logger
	cache  *cache.Cache
}

func NewECSCache(logger log.Logger, client ECS) *ECSCache {
	return &ECSCache{
		client: client,
		logger: logger,
		cache: cache.New(
			cache.WithDefaultExpiration(15*time.Minute),
			cache.WithJanitor(30*time.Minute),
			cache.WithGetReturnStale(),
		),
	}
}

func NewDefaultECS() *ecs.Client {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}

	return ecs.NewFromConfig(cfg)
}

func (ec *ECSCache) DescribeClusters(
	ctx context.Context, params *ecs.DescribeClustersInput, optFns ...func(*ecs.Options),
) (*ecs.DescribeClustersOutput, error) {
	cached, ok := ec.cache.Get("DescribeClusters")
	if ok {
		ec.logger.Debug("ECS DescribeClusters cache hit", "clusters", params.Clusters)
		return cached.(*ecs.DescribeClustersOutput), nil //nolint:forcetypeassert
	}

	v, err := ec.client.DescribeClusters(ctx, params, optFns...)
	if err != nil {
		if cached != nil {
			ec.logger.Warn("ECS DescribeClusters failed, stale cache response found and used",
				"clusters", params.Clusters, "err", err)
			return cached.(*ecs.DescribeClustersOutput), nil //nolint:forcetypeassert
		}
	}
	ec.cache.Set("DescribeClusters", v, time.Hour)
	return v, err
}

func (ec *ECSCache) ListServices(ctx context.Context, params *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error) {
	cluster := *params.Cluster
	cached, ok := ec.cache.Get("ListServices-" + cluster)

	if ok {
		ec.logger.Debug("ECS ListServices cache hit", "cluster", cluster)
		return cached.(*ecs.ListServicesOutput), nil //nolint:forcetypeassert
	}

	v, err := ec.client.ListServices(ctx, params, optFns...)
	if err != nil {
		if cached != nil {
			ec.logger.Warn("ECS ListServices failed, stale cache response found and used",
				"cluster", cluster, "err", err)
			return cached.(*ecs.ListServicesOutput), nil //nolint:forcetypeassert
		}

		return v, err
	}

	ec.cache.Set("ListServices-"+cluster, v, 5*time.Minute)
	return v, nil
}

func (ec *ECSCache) ListTasks(ctx context.Context, params *ecs.ListTasksInput, optFns ...func(*ecs.Options)) (*ecs.ListTasksOutput, error) {
	return ec.client.ListTasks(ctx, params, optFns...)
}

func (ec *ECSCache) DescribeTasks(ctx context.Context, params *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
	cachedTasks := make([]types.Task, 0)
	uncachedTasks := make([]string, 0)
	if params != nil && params.Tasks != nil {
		for _, task := range params.Tasks {
			if v, ok := ec.cache.Get("DescribeTasks-" + task); ok {
				ec.logger.Debug("ECS DescribeTasks cache hit", "cluster", *params.Cluster, "task", task)
				if t, ok := v.(*types.Task); ok {
					cachedTasks = append(cachedTasks, *t)
				}
				continue
			}
			uncachedTasks = append(uncachedTasks, task)
		}
	}

	if len(uncachedTasks) == 0 {
		return &ecs.DescribeTasksOutput{Tasks: cachedTasks}, nil
	}

	if params == nil {
		params = &ecs.DescribeTasksInput{}
	}

	params.Tasks = uncachedTasks
	response, err := ec.client.DescribeTasks(ctx, params, optFns...)
	if err != nil {
		return response, err
	}
	for _, task := range response.Tasks {
		ec.cache.Set("DescribeTasks-"+*task.TaskArn, &task, time.Minute)
	}

	response.Tasks = append(response.Tasks, cachedTasks...)
	return response, nil
}

func (ec *ECSCache) ListTagsForResource(
	ctx context.Context, params *ecs.ListTagsForResourceInput, optFns ...func(*ecs.Options),
) (*ecs.ListTagsForResourceOutput, error) {
	arn := *params.ResourceArn
	cached, ok := ec.cache.Get("ListTagsForResource-" + arn)
	if ok {
		ec.logger.Debug("ECS ListTagsForResource cache hit", "arn", arn)
		return cached.(*ecs.ListTagsForResourceOutput), nil //nolint:forcetypeassert
	}

	v, err := ec.client.ListTagsForResource(ctx, params, optFns...)
	if err != nil {
		if cached != nil {
			ec.logger.Warn("ECS ListTagsForResource failed, stale cache response found and used", "err", err)
			return cached.(*ecs.ListTagsForResourceOutput), nil //nolint:forcetypeassert
		}
		return v, err
	}

	ec.cache.SetDefault("ListTagsForResource-"+arn, v)
	return v, nil
}
