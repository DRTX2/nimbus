package workflow

import "testing"

func TestNormalizeAndValidate(t *testing.T) {
	tests := []struct {
		name    string
		def     Definition
		wantErr bool
	}{
		{
			name: "valid parallel dag",
			def: Definition{EventType: "order.created", Steps: []Step{
				{ID: "start", Type: "noop"},
				{ID: "notify", Type: "noop", DependsOn: []string{"start"}},
				{ID: "audit", Type: "noop", DependsOn: []string{"start"}},
			}},
		},
		{
			name: "cycle",
			def: Definition{EventType: "test", Steps: []Step{
				{ID: "one", Type: "noop", DependsOn: []string{"two"}},
				{ID: "two", Type: "noop", DependsOn: []string{"one"}},
			}},
			wantErr: true,
		},
		{
			name: "missing dependency",
			def: Definition{EventType: "test", Steps: []Step{
				{ID: "one", Type: "noop", DependsOn: []string{"missing"}},
			}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.def.NormalizeAndValidate()
			if (err != nil) != test.wantErr {
				t.Fatalf("NormalizeAndValidate() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && test.def.SchemaVersion != SchemaVersion {
				t.Fatalf("schema version = %d, want %d", test.def.SchemaVersion, SchemaVersion)
			}
		})
	}
}

func TestNormalizeHTTPDefaults(t *testing.T) {
	definition := Definition{EventType: "test", Steps: []Step{{
		ID: "send", Type: "http", HTTP: &HTTPRequest{URL: "https://example.com/hook"},
	}}}
	if err := definition.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if got := definition.Steps[0].HTTP.Method; got != "POST" {
		t.Fatalf("HTTP method = %q, want POST", got)
	}
}
