package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/david/nimbus/internal/store"
)

// Connector executes one step type. Implementations must honor ctx deadlines,
// bound every response they read, and classify failures with StepError.
// New step types (for example Slack or SQS delivery) register here without
// changing the worker loop.
type Connector interface {
	Execute(ctx context.Context, job store.Job) (json.RawMessage, error)
}

// ConnectorFunc adapts a function to Connector.
type ConnectorFunc func(context.Context, store.Job) (json.RawMessage, error)

func (f ConnectorFunc) Execute(ctx context.Context, job store.Job) (json.RawMessage, error) {
	return f(ctx, job)
}

// StepError carries the retry classification of a connector failure.
type StepError struct {
	Err       error
	Retryable bool
}

func (e *StepError) Error() string { return e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

func retryable(err error) error { return &StepError{Err: err, Retryable: true} }
func permanent(err error) error { return &StepError{Err: err} }

// IsRetryable reports whether a connector failure may be retried. Unclassified
// errors are permanent so unknown failures never multiply external effects.
func IsRetryable(err error) bool {
	var stepErr *StepError
	return errors.As(err, &stepErr) && stepErr.Retryable
}

// Registry maps step types to connectors.
type Registry map[string]Connector

func (r Registry) execute(ctx context.Context, job store.Job) (json.RawMessage, error) {
	connector, ok := r[job.StepType]
	if !ok {
		return nil, permanent(fmt.Errorf("unsupported step type %q", job.StepType))
	}
	return connector.Execute(ctx, job)
}

var noopConnector = ConnectorFunc(func(context.Context, store.Job) (json.RawMessage, error) {
	return json.RawMessage(`{"result":"noop"}`), nil
})
