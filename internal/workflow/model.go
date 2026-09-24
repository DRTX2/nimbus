package workflow

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const SchemaVersion = 1

type Definition struct {
	SchemaVersion int    `json:"schemaVersion,omitempty" doc:"Workflow contract version; defaults to the latest." example:"1"`
	EventType     string `json:"eventType" minLength:"1" maxLength:"100" example:"order.created"`
	Steps         []Step `json:"steps" minItems:"1" maxItems:"50"`
}

type Step struct {
	ID             string       `json:"id" minLength:"1" maxLength:"64" pattern:"^[a-z][a-z0-9_-]*$" example:"notify"`
	Type           string       `json:"type" enum:"noop,http" example:"http"`
	DependsOn      []string     `json:"dependsOn,omitempty"`
	TimeoutSeconds int          `json:"timeoutSeconds,omitempty" minimum:"1" maximum:"30" default:"10"`
	Retry          RetryPolicy  `json:"retry,omitempty"`
	HTTP           *HTTPRequest `json:"http,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts         int `json:"maxAttempts,omitempty" minimum:"1" maximum:"10" default:"3"`
	InitialDelaySeconds int `json:"initialDelaySeconds,omitempty" minimum:"1" maximum:"60" default:"2"`
	MaxDelaySeconds     int `json:"maxDelaySeconds,omitempty" minimum:"1" maximum:"300" default:"60"`
}

type HTTPRequest struct {
	Method   string `json:"method,omitempty" enum:"POST,PUT,PATCH" default:"POST"`
	URL      string `json:"url" format:"uri" example:"https://httpbin.org/status/204"`
	Body     string `json:"body,omitempty" maxLength:"65536"`
	BodyMode string `json:"bodyMode,omitempty" enum:"static,event" default:"static"`
}

func (d *Definition) NormalizeAndValidate() error {
	if d.SchemaVersion == 0 {
		d.SchemaVersion = SchemaVersion
	}
	if d.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schemaVersion %d", d.SchemaVersion)
	}
	d.EventType = strings.TrimSpace(d.EventType)
	if d.EventType == "" || len(d.EventType) > 100 {
		return errors.New("eventType must contain between 1 and 100 characters")
	}
	if len(d.Steps) == 0 || len(d.Steps) > 50 {
		return errors.New("workflow must contain between 1 and 50 steps")
	}

	steps := make(map[string]*Step, len(d.Steps))
	for i := range d.Steps {
		step := &d.Steps[i]
		step.ID = strings.TrimSpace(step.ID)
		if !validStepID(step.ID) {
			return fmt.Errorf("step %q has an invalid id", step.ID)
		}
		if _, exists := steps[step.ID]; exists {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		steps[step.ID] = step
		if step.TimeoutSeconds == 0 {
			step.TimeoutSeconds = 10
		}
		if step.TimeoutSeconds < 1 || step.TimeoutSeconds > 30 {
			return fmt.Errorf("step %q timeoutSeconds must be between 1 and 30", step.ID)
		}
		if step.Retry.MaxAttempts == 0 {
			step.Retry.MaxAttempts = 3
		}
		if step.Retry.InitialDelaySeconds == 0 {
			step.Retry.InitialDelaySeconds = 2
		}
		if step.Retry.MaxDelaySeconds == 0 {
			step.Retry.MaxDelaySeconds = 60
		}
		if step.Retry.MaxAttempts < 1 || step.Retry.MaxAttempts > 10 ||
			step.Retry.InitialDelaySeconds < 1 || step.Retry.InitialDelaySeconds > 60 ||
			step.Retry.MaxDelaySeconds < step.Retry.InitialDelaySeconds || step.Retry.MaxDelaySeconds > 300 {
			return fmt.Errorf("step %q has an invalid retry policy", step.ID)
		}
		if err := validateAction(step); err != nil {
			return fmt.Errorf("step %q: %w", step.ID, err)
		}
		slices.Sort(step.DependsOn)
	}

	for _, step := range d.Steps {
		seen := make(map[string]struct{}, len(step.DependsOn))
		for _, dependency := range step.DependsOn {
			if dependency == step.ID {
				return fmt.Errorf("step %q cannot depend on itself", step.ID)
			}
			if _, exists := steps[dependency]; !exists {
				return fmt.Errorf("step %q references missing dependency %q", step.ID, dependency)
			}
			if _, duplicate := seen[dependency]; duplicate {
				return fmt.Errorf("step %q repeats dependency %q", step.ID, dependency)
			}
			seen[dependency] = struct{}{}
		}
	}
	if hasCycle(d.Steps) {
		return errors.New("workflow contains a dependency cycle")
	}
	return nil
}

func validateAction(step *Step) error {
	switch step.Type {
	case "noop":
		if step.HTTP != nil {
			return errors.New("noop must not include http configuration")
		}
	case "http":
		if step.HTTP == nil {
			return errors.New("http configuration is required")
		}
		if step.HTTP.Method == "" {
			step.HTTP.Method = http.MethodPost
		}
		if step.HTTP.BodyMode == "" {
			step.HTTP.BodyMode = "static"
		}
		if step.HTTP.BodyMode != "static" && step.HTTP.BodyMode != "event" {
			return errors.New("HTTP bodyMode must be static or event")
		}
		if step.HTTP.Method != http.MethodPost && step.HTTP.Method != http.MethodPut && step.HTTP.Method != http.MethodPatch {
			return errors.New("HTTP method must be POST, PUT, or PATCH")
		}
		parsed, err := url.ParseRequestURI(step.HTTP.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return errors.New("HTTP URL must be an absolute http or https URL")
		}
		if parsed.User != nil {
			return errors.New("HTTP URL must not contain user credentials")
		}
		if len(step.HTTP.Body) > 64<<10 {
			return errors.New("HTTP body exceeds 64 KiB")
		}
	default:
		return fmt.Errorf("unsupported type %q", step.Type)
	}
	return nil
}

func validStepID(id string) bool {
	if len(id) == 0 || len(id) > 64 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for _, char := range id[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func hasCycle(steps []Step) bool {
	dependencies := make(map[string][]string, len(steps))
	for _, step := range steps {
		dependencies[step.ID] = step.DependsOn
	}
	visiting := make(map[string]bool, len(steps))
	visited := make(map[string]bool, len(steps))
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, dependency := range dependencies[id] {
			if visit(dependency) {
				return true
			}
		}
		visiting[id] = false
		visited[id] = true
		return false
	}
	for id := range dependencies {
		if visit(id) {
			return true
		}
	}
	return false
}
