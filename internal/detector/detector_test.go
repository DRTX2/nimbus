package detector

import "testing"

func TestEvaluate(t *testing.T) {
	observation := Observation{
		VisibleMessages: 200, OldestMessageSeconds: 600, DLQMessages: 3,
		LambdaInvocations: 20, LambdaErrors: 15, LambdaThrottles: 2,
	}
	decisions := Evaluate(observation, Thresholds{Backlog: 100, Age: 300})
	breached := map[string]bool{}
	for _, decision := range decisions {
		breached[decision.Detector] = decision.Breached
	}
	for _, name := range []string{"lambda_throttled", "dlq_growth", "retry_storm"} {
		if !breached[name] {
			t.Fatalf("detector %s did not breach", name)
		}
	}
	if breached["consumer_stalled"] {
		t.Fatal("consumer_stalled breached despite active invocations")
	}
}
