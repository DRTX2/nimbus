package detector

type Observation struct {
	VisibleMessages      int64
	OldestMessageSeconds int64
	DLQMessages          int64
	LambdaInvocations    int64
	LambdaErrors         int64
	LambdaThrottles      int64
}

type Thresholds struct {
	Backlog int64
	Age     int64
}

type Decision struct {
	Detector string
	Severity string
	Summary  string
	Breached bool
}

// Evaluate returns explainable, deterministic decisions from one complete metric window.
func Evaluate(observation Observation, thresholds Thresholds) []Decision {
	return []Decision{
		{
			Detector: "consumer_stalled", Severity: "critical",
			Summary:  "Queue backlog is aging while the Lambda consumer has no invocations",
			Breached: observation.VisibleMessages >= thresholds.Backlog && observation.OldestMessageSeconds >= thresholds.Age && observation.LambdaInvocations == 0,
		},
		{
			Detector: "lambda_throttled", Severity: "warning",
			Summary:  "Lambda throttling is sustained while queue work remains",
			Breached: observation.LambdaThrottles > 0 && observation.VisibleMessages > 0,
		},
		{
			Detector: "dlq_growth", Severity: "critical",
			Summary:  "The dead-letter queue contains messages requiring investigation",
			Breached: observation.DLQMessages > 0,
		},
		{
			Detector: "retry_storm", Severity: "warning",
			Summary:  "Lambda errors are high while the queue backlog remains elevated",
			Breached: observation.LambdaInvocations >= 10 && observation.LambdaErrors*2 >= observation.LambdaInvocations && observation.VisibleMessages >= thresholds.Backlog,
		},
	}
}
