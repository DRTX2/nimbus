//go:build acceptance

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// world holds the state of one scenario. Every scenario receives its own
// tenant, credentials, and variables so scenarios can run in any order.
type world struct {
	env        *environment
	client     *http.Client
	scenarioID string
	tenantID   string
	tokens     map[string]string
	actor      string
	vars       map[string]string
	status     int
	body       []byte
	statuses   []int
}

func newWorld(env *environment) *world {
	return &world{
		env: env, client: &http.Client{Timeout: 20 * time.Second},
		scenarioID: randomHex(4), tokens: map[string]string{}, vars: map[string]string{},
	}
}

var placeholder = regexp.MustCompile(`\{([A-Za-z0-9_.:-]+)\}`)

// interpolate replaces {name} with remembered variables so feature files can
// refer to identifiers created earlier in the scenario.
func (w *world) interpolate(value string) (string, error) {
	var missing []string
	result := placeholder.ReplaceAllStringFunc(value, func(match string) string {
		key := match[1 : len(match)-1]
		if resolved, ok := w.vars[key]; ok {
			return resolved
		}
		missing = append(missing, key)
		return match
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unknown scenario variables %v", missing)
	}
	return result, nil
}

func (w *world) request(ctx context.Context, method, path string, body any, headers map[string]string) error {
	return w.requestAs(ctx, w.actor, method, path, body, headers)
}

func (w *world) requestAs(ctx context.Context, actor, method, path string, body any, headers map[string]string) error {
	var reader io.Reader
	switch value := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(value)
	case []byte:
		reader = bytes.NewReader(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, w.env.baseURL+path, reader)
	if err != nil {
		return err
	}
	if reader != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token := w.tokens[actor]; token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := w.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	w.status = response.StatusCode
	w.body, err = io.ReadAll(io.LimitReader(response.Body, 4<<20))
	w.statuses = append(w.statuses, response.StatusCode)
	return err
}

func (w *world) expectStatus(want int) error {
	if w.status != want {
		return fmt.Errorf("status = %d, want %d; body: %s", w.status, want, truncate(w.body))
	}
	return nil
}

func (w *world) field(path string) (any, error) {
	var document any
	if err := json.Unmarshal(w.body, &document); err != nil {
		return nil, fmt.Errorf("response is not JSON: %s", truncate(w.body))
	}
	return lookup(document, path)
}

func lookup(document any, path string) (any, error) {
	current := document
	for _, part := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[part]
			if !ok {
				return nil, fmt.Errorf("field %q not found", path)
			}
			current = value
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return nil, fmt.Errorf("index %q out of range in %q", part, path)
			}
			current = node[index]
		default:
			return nil, fmt.Errorf("cannot descend into %q", path)
		}
	}
	return current, nil
}

func (w *world) stringField(path string) (string, error) {
	value, err := w.field(path)
	if err != nil {
		return "", err
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case nil:
		return "", nil
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded), nil
	}
}

// eventually polls check until it succeeds or the timeout elapses, returning
// the last failure for a precise report.
func eventually(ctx context.Context, timeout time.Duration, check func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := check()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("condition not met within %s: %w", timeout, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func truncate(body []byte) string {
	if len(body) > 600 {
		return string(body[:600]) + "..."
	}
	return string(body)
}

func registerHTTPSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^I send a (GET|POST|PATCH|DELETE) request to "([^"]*)"$`, func(ctx context.Context, method, path string) error {
		resolved, err := w.interpolate(path)
		if err != nil {
			return err
		}
		return w.request(ctx, method, resolved, nil, nil)
	})
	sc.Step(`^I send a (POST|PATCH) request to "([^"]*)" with:$`, func(ctx context.Context, method, path string, body *godog.DocString) error {
		resolvedPath, err := w.interpolate(path)
		if err != nil {
			return err
		}
		resolvedBody, err := w.interpolate(body.Content)
		if err != nil {
			return err
		}
		return w.request(ctx, method, resolvedPath, resolvedBody, nil)
	})
	sc.Step(`^I send an unauthenticated GET request to "([^"]*)"$`, func(ctx context.Context, path string) error {
		return w.requestAs(ctx, "", http.MethodGet, path, nil, nil)
	})
	sc.Step(`^the response status should be (\d+)$`, w.expectStatus)
	sc.Step(`^the response field "([^"]*)" should be "([^"]*)"$`, func(path, want string) error {
		want, err := w.interpolate(want)
		if err != nil {
			return err
		}
		got, err := w.stringField(path)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s = %q, want %q", path, got, want)
		}
		return nil
	})
	sc.Step(`^the response field "([^"]*)" should be (true|false)$`, func(path, want string) error {
		got, err := w.stringField(path)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s = %s, want %s", path, got, want)
		}
		return nil
	})
	sc.Step(`^the response field "([^"]*)" should contain "([^"]*)"$`, func(path, want string) error {
		got, err := w.stringField(path)
		if err != nil {
			return err
		}
		if !strings.Contains(got, want) {
			return fmt.Errorf("%s = %q, want it to contain %q", path, got, want)
		}
		return nil
	})
	sc.Step(`^the response should list (\d+) items?$`, func(want int) error {
		items, err := w.field("items")
		if err != nil {
			return err
		}
		list, ok := items.([]any)
		if !ok || len(list) != want {
			return fmt.Errorf("items = %v, want %d entries", items, want)
		}
		return nil
	})
	sc.Step(`^I remember the response field "([^"]*)" as "([^"]*)"$`, func(path, name string) error {
		value, err := w.stringField(path)
		if err != nil {
			return err
		}
		w.vars[name] = value
		return nil
	})
}
