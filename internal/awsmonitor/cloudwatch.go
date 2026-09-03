package awsmonitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/david/nimbus/internal/store"
)

type cloudWatchAPI interface {
	GetMetricData(context.Context, *cloudwatch.GetMetricDataInput, ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
}

type Collector struct {
	externalIDs map[string]string
}

func NewCollector(externalIDs map[string]string) *Collector {
	configured := make(map[string]string, len(externalIDs))
	for ref, value := range externalIDs {
		configured[ref] = value
	}
	return &Collector{externalIDs: configured}
}

func (c *Collector) Collect(ctx context.Context, pipeline store.Pipeline) (store.Observation, error) {
	if pipeline.Mode != "aws" {
		return store.Observation{}, fmt.Errorf("pipeline %s is not an AWS pipeline", pipeline.ID)
	}
	if pipeline.RoleARN != "" && pipeline.ExternalIDRef == "" {
		return store.Observation{}, errors.New("AWS external ID reference is required")
	}
	externalID, err := c.resolveExternalID(pipeline.ExternalIDRef)
	if err != nil {
		return store.Observation{}, err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(pipeline.Region))
	if err != nil {
		return store.Observation{}, fmt.Errorf("load AWS configuration: %w", err)
	}
	if pipeline.RoleARN != "" {
		cfg.Credentials = aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), pipeline.RoleARN, func(options *stscreds.AssumeRoleOptions) {
			if externalID != "" {
				options.ExternalID = aws.String(externalID)
			}
		}))
	}
	return collect(ctx, cloudwatch.NewFromConfig(cfg), pipeline, collectionWindowEnd(time.Now()))
}

func collectionWindowEnd(now time.Time) time.Time {
	return now.UTC().Truncate(5 * time.Minute).Add(-5 * time.Minute)
}

func (c *Collector) resolveExternalID(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	value, ok := c.externalIDs[ref]
	if !ok {
		return "", errors.New("AWS external ID reference is not configured")
	}
	return value, nil
}

func collect(ctx context.Context, client cloudWatchAPI, pipeline store.Pipeline, end time.Time) (store.Observation, error) {
	start := end.Add(-5 * time.Minute)
	period := int32(300)
	queue, function, dlq := resourceName(pipeline.QueueARN), resourceName(pipeline.LambdaARN), resourceName(pipeline.DLQARN)
	queries := []types.MetricDataQuery{
		metric("visible", "AWS/SQS", "ApproximateNumberOfMessagesVisible", "QueueName", queue, "Maximum", period),
		metric("inflight", "AWS/SQS", "ApproximateNumberOfMessagesNotVisible", "QueueName", queue, "Maximum", period),
		metric("oldest", "AWS/SQS", "ApproximateAgeOfOldestMessage", "QueueName", queue, "Maximum", period),
		metric("dlq", "AWS/SQS", "ApproximateNumberOfMessagesVisible", "QueueName", dlq, "Maximum", period),
		metric("invocations", "AWS/Lambda", "Invocations", "FunctionName", function, "Sum", period),
		metric("errors", "AWS/Lambda", "Errors", "FunctionName", function, "Sum", period),
		metric("throttles", "AWS/Lambda", "Throttles", "FunctionName", function, "Sum", period),
	}
	output, err := client.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{StartTime: &start, EndTime: &end, MetricDataQueries: queries, ScanBy: types.ScanByTimestampDescending, MaxDatapoints: aws.Int32(100)})
	if err != nil {
		return store.Observation{}, fmt.Errorf("read bounded CloudWatch metrics: %w", err)
	}
	values := map[string]int64{}
	for _, result := range output.MetricDataResults {
		if result.StatusCode != types.StatusCodeComplete || len(result.Values) == 0 || len(result.Timestamps) == 0 {
			continue
		}
		values[aws.ToString(result.Id)] = int64(result.Values[0])
	}
	for _, id := range []string{"visible", "inflight", "oldest", "dlq", "invocations", "errors", "throttles"} {
		if _, ok := values[id]; !ok {
			return store.Observation{}, fmt.Errorf("CloudWatch returned an incomplete metric window: missing %s", id)
		}
	}
	return store.Observation{PipelineID: pipeline.ID, WindowStartedAt: start, WindowEndedAt: end,
		VisibleMessages: values["visible"], InFlightMessages: values["inflight"], OldestMessageSeconds: values["oldest"],
		DLQMessages: values["dlq"], LambdaInvocations: values["invocations"], LambdaErrors: values["errors"],
		LambdaThrottles: values["throttles"], Source: "cloudwatch"}, nil
}

func metric(id, namespace, name, dimension, value, stat string, period int32) types.MetricDataQuery {
	return types.MetricDataQuery{Id: aws.String(id), ReturnData: aws.Bool(true), MetricStat: &types.MetricStat{Metric: &types.Metric{
		Namespace: aws.String(namespace), MetricName: aws.String(name), Dimensions: []types.Dimension{{Name: aws.String(dimension), Value: aws.String(value)}},
	}, Period: aws.Int32(period), Stat: aws.String(stat)}}
}

func resourceName(arn string) string {
	parts := strings.Split(arn, ":")
	value := parts[len(parts)-1]
	if slash := strings.LastIndex(value, "/"); slash >= 0 {
		return value[slash+1:]
	}
	return value
}
