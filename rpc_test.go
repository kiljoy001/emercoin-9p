package emercoin9p

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()

	saved := make(map[string]*string, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			v := value
			saved[key] = &v
		} else {
			saved[key] = nil
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}

	t.Cleanup(func() {
		for _, key := range keys {
			if value := saved[key]; value != nil {
				_ = os.Setenv(key, *value)
				continue
			}
			_ = os.Unsetenv(key)
		}
	})
}

func TestEmercoinRPCSendReturnsJSONRPCResult(t *testing.T) {
	client := NewEmercoinRPC("http://rpc.example.test", "", "user", "pass")
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			user, pass, ok := r.BasicAuth()
			if !ok {
				t.Fatalf("missing basic auth")
			}
			if user != "user" || pass != "pass" {
				t.Fatalf("unexpected credentials: %q/%q", user, pass)
			}
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}

			var payload struct {
				Method string `json:"method"`
				Params []any  `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("failed to decode request payload: %v", err)
			}
			if payload.Method != "name_show" {
				t.Fatalf("unexpected method: %q", payload.Method)
			}
			if len(payload.Params) != 1 || payload.Params[0] != "dns:rentonsoftworks.coin" {
				t.Fatalf("unexpected params: %#v", payload.Params)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"result":{"name":"dns:rentonsoftworks.coin","value":"1.2.3.4"},"error":null,"id":1}`,
				)),
			}, nil
		}),
	}

	result, err := client.Send(context.Background(), Query{
		Cmd:  "name_show",
		Args: []any{"dns:rentonsoftworks.coin"},
	})
	if err != nil {
		t.Fatalf("Send() returned unexpected error: %v", err)
	}

	expected := `{"name":"dns:rentonsoftworks.coin","value":"1.2.3.4"}`
	if string(result) != expected {
		t.Fatalf("unexpected result: got %q want %q", string(result), expected)
	}
}

func TestDecodeRPCResponseError(t *testing.T) {
	_, err := decodeRPCResponse([]byte(`{"result":null,"error":{"code":-1,"message":"boom"},"id":1}`))
	if err == nil {
		t.Fatalf("expected rpc error")
	}
	if err.Error() != "boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadDotEnvFileLoadsRPCConfig(t *testing.T) {
	unsetEnv(t, "EMC_RPC_URL", "EMC_RPC_LOCATION", "EMC_RPC_HOST", "EMC_RPC_PORT", "EMC_RPC_USER", "EMC_RPC_PASS")

	envPath := filepath.Join(t.TempDir(), "rpc.env")
	content := strings.Join([]string{
		"EMC_RPC_URL=http://rpc.from.dotenv",
		"EMC_RPC_PORT=7777",
		"EMC_RPC_USER=test-user",
		`EMC_RPC_PASS="secret-pass"`,
	}, "\n")
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp .env: %v", err)
	}

	if err := loadDotEnvFile(envPath); err != nil {
		t.Fatalf("failed to load temp .env: %v", err)
	}

	cfg, err := rpcConfigFromEnvironment()
	if err != nil {
		t.Fatalf("failed to read rpc config from env: %v", err)
	}

	if cfg.Location != "http://rpc.from.dotenv" {
		t.Fatalf("unexpected location: %q", cfg.Location)
	}
	if cfg.Port != "7777" {
		t.Fatalf("unexpected port: %q", cfg.Port)
	}
	if cfg.User != "test-user" {
		t.Fatalf("unexpected user: %q", cfg.User)
	}
	if cfg.Password != "secret-pass" {
		t.Fatalf("unexpected password: %q", cfg.Password)
	}
}

func TestNewEmercoinRPCFromEnv(t *testing.T) {
	t.Setenv("EMC_RPC_URL", "http://rpc.example.test")
	t.Setenv("EMC_RPC_PORT", "9999")
	t.Setenv("EMC_RPC_USER", "user")
	t.Setenv("EMC_RPC_PASS", "pass")

	client, err := NewEmercoinRPCFromEnv()
	if err != nil {
		t.Fatalf("failed to create rpc client from env: %v", err)
	}

	if client.Location != "http://rpc.example.test" {
		t.Fatalf("unexpected location: %q", client.Location)
	}
	if client.Port != "9999" {
		t.Fatalf("unexpected port: %q", client.Port)
	}
	if client.RPCUser != "user" {
		t.Fatalf("unexpected rpc user: %q", client.RPCUser)
	}
	if client.RPCPassword != "pass" {
		t.Fatalf("unexpected rpc password: %q", client.RPCPassword)
	}
}

func TestNewNsFromEnvConfiguresBackend(t *testing.T) {
	t.Setenv("EMC_RPC_URL", "http://rpc.example.test")
	t.Setenv("EMC_RPC_PORT", "9999")
	t.Setenv("EMC_RPC_USER", "user")
	t.Setenv("EMC_RPC_PASS", "pass")

	ns, err := NewNsFromEnv()
	if err != nil {
		t.Fatalf("failed to create namespace from env: %v", err)
	}
	if ns.config.backend == nil {
		t.Fatalf("expected namespace backend to be configured")
	}
}

func TestNameShowReturns(t *testing.T) {
	client, err := NewEmercoinRPCFromEnv()
	if err != nil {
		t.Skipf("Skipping live rpc test: %v", err)
	}

	ctx := context.Background()

	var query Query
	query.Method("name_show")
	query.Params([]any{"dns:rentonsoftworks.coin"})
	result, err := client.Send(ctx, query)
	if err != nil {
		t.Fatalf("Send() returned unexpected error: %v", err)
	}

	var nameData struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(result, &nameData); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	expectedName := "dns:rentonsoftworks.coin"
	if nameData.Name != expectedName {
		t.Errorf("Expected name %q, got %q", expectedName, nameData.Name)
	}
	if nameData.Value == "" {
		t.Error("Expected non-empty value")
	}
}
