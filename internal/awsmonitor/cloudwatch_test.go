package awsmonitor

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/david/nimbus/internal/store"
)

type fakeCloudWatch struct{ queries int }

func (f *fakeCloudWatch) GetMetricData(_ context.Context, input *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	f.queries = len(input.MetricDataQueries)
	timestamp := time.Unix(1_700_000_000, 0)
	results := make([]types.MetricDataResult, 0, len(input.MetricDataQueries))
	for _, query := range input.MetricDataQueries {
		value := float64(0)
		if aws.ToString(query.Id) == "visible" {
			value = 42
		}
		if aws.ToString(query.Id) == "errors" {
			value = 3
		}
		results = append(results, types.MetricDataResult{Id: query.Id, Values: []float64{value}, Timestamps: []time.Time{timestamp}, StatusCode: types.StatusCodeComplete})
	}
	return &cloudwatch.GetMetricDataOutput{MetricDataResults: results}, nil
}

func TestCollectUsesBoundedMetricBatch(t *testing.T) {
	client := &fakeCloudWatch{}
	observation, err := collect(context.Background(), client, store.Pipeline{
		ID: "pipeline", QueueARN: "arn:aws:sqs:us-east-1:1:orders", DLQARN: "arn:aws:sqs:us-east-1:1:orders-dlq",
		LambdaARN: "arn:aws:lambda:us-east-1:1:function:consumer",
	}, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("collect() error = %v", err)
	}
	if client.queries != 7 {
		t.Fatalf("queries = %d, want 7", client.queries)
	}
	if observation.VisibleMessages != 42 || observation.LambdaErrors != 3 {
		t.Fatalf("unexpected observation: %+v", observation)
	}
}

func TestResourceName(t *testing.T) {
	if got := resourceName("arn:aws:lambda:us-east-1:1:function:consumer"); got != "consumer" {
		t.Fatalf("resourceName = %q", got)
	}
}
