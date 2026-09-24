//go:build acceptance

// Package acceptance runs the Gherkin specifications in test/features against
// a real Nimbus system. See docs/TESTING.md for the strategy and commands.
package acceptance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	env, err := newEnvironment(ctx)
	if err != nil {
		t.Fatalf("start acceptance environment: %v", err)
	}
	defer env.shutdown()

	tags := os.Getenv("NIMBUS_ACCEPTANCE_TAGS")
	if env.remote {
		// A deployed system has one tenant and no scripted collaborators, so
		// only scenarios that are safe against shared state run remotely.
		tags = joinTags("@smoke", tags)
	}
	tags = joinTags(tags, "~@wip")
	format := os.Getenv("NIMBUS_ACCEPTANCE_FORMAT")
	if format == "" {
		format = "pretty"
	}

	suite := godog.TestSuite{
		Name: "nimbus",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			w := newWorld(env)
			registerHTTPSteps(sc, w)
			registerIdentitySteps(sc, w)
			registerWorkflowSteps(sc, w)
			registerIncidentSteps(sc, w)
		},
		Options: &godog.Options{
			Format:   format,
			Paths:    []string{"../features"},
			Tags:     tags,
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("acceptance scenarios failed")
	}
}

func joinTags(left, right string) string {
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + " && " + right
	}
}
