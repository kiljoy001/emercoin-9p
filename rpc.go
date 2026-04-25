package emercoin9p

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultRPCLocation = "http://rentonsoftworks.coin"
	defaultRPCPort     = "6662"
)

var (
	defaultDotEnvOnce sync.Once
	defaultDotEnvErr  error
)

type RPCConfig struct {
	Location string
	Port     string
	User     string
	Password string
}

type EmercoinRPC struct {
	Location    string
	Port        string
	RPCUser     string
	RPCPassword string
	HTTPClient  *http.Client
}

func NewEmercoinRPC(loc string, port string, user string, pass string) EmercoinRPC {
	return EmercoinRPC{
		Location:    loc,
		Port:        port,
		RPCUser:     user,
		RPCPassword: pass,
	}
}

func NewEmercoinRPCFromConfig(cfg RPCConfig) EmercoinRPC {
	return NewEmercoinRPC(cfg.Location, cfg.Port, cfg.User, cfg.Password)
}

func NewEmercoinRPCFromEnv() (EmercoinRPC, error) {
	cfg, err := LoadRPCConfig()
	if err != nil {
		return EmercoinRPC{}, err
	}
	return NewEmercoinRPCFromConfig(cfg), nil
}

// WithRPCBackendFromEnv configures a namespace to use RPC credentials from environment or .env.
func WithRPCBackendFromEnv() (NamespaceOption, error) {
	backend, err := NewEmercoinRPCFromEnv()
	if err != nil {
		return nil, err
	}
	return WithRPCBackend(backend), nil
}

// NewNsFromEnv creates a namespace with an RPC backend loaded from environment or .env.
func NewNsFromEnv(opts ...NamespaceOption) (*Namespace, error) {
	backend, err := NewEmercoinRPCFromEnv()
	if err != nil {
		return nil, err
	}
	opts = append([]NamespaceOption{WithRPCBackend(backend)}, opts...)

	authCfg, err := authConfigFromEnvironment()
	if err != nil {
		return nil, err
	}
	if authCfg != nil {
		opts = append(opts, With9FrontAuth(authCfg.Domain, authCfg.User, authCfg.Password))
	}

	return NewNs(opts...), nil
}

func LoadRPCConfig() (RPCConfig, error) {
	if err := loadDefaultDotEnv(); err != nil {
		return RPCConfig{}, err
	}
	return rpcConfigFromEnvironment()
}

func loadDefaultDotEnv() error {
	defaultDotEnvOnce.Do(func() {
		defaultDotEnvErr = loadDotEnvFile(".env")
		if defaultDotEnvErr != nil && os.IsNotExist(defaultDotEnvErr) {
			defaultDotEnvErr = nil
		}
	})
	return defaultDotEnvErr
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid .env line: %q", line)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			return fmt.Errorf("invalid empty .env key")
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func rpcConfigFromEnvironment() (RPCConfig, error) {
	location := firstNonEmpty(
		os.Getenv("EMC_RPC_URL"),
		os.Getenv("EMC_RPC_LOCATION"),
		os.Getenv("EMC_RPC_HOST"),
		defaultRPCLocation,
	)
	port := firstNonEmpty(os.Getenv("EMC_RPC_PORT"), defaultRPCPort)
	user := os.Getenv("EMC_RPC_USER")
	password := os.Getenv("EMC_RPC_PASS")

	if user == "" {
		return RPCConfig{}, fmt.Errorf("EMC_RPC_USER is not set")
	}
	if password == "" {
		return RPCConfig{}, fmt.Errorf("EMC_RPC_PASS is not set")
	}

	return RPCConfig{
		Location: location,
		Port:     port,
		User:     user,
		Password: password,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func authConfigFromEnvironment() (*nineFrontAuthConfig, error) {
	domain := firstNonEmpty(
		os.Getenv("EMC_9FRONT_AUTH_DOM"),
		os.Getenv("EMC_9FRONT_AUTH_DOMAIN"),
	)
	user := os.Getenv("EMC_9FRONT_AUTH_USER")
	password := os.Getenv("EMC_9FRONT_AUTH_PASS")

	if domain == "" && user == "" && password == "" {
		return nil, nil
	}
	if domain == "" || user == "" || password == "" {
		return nil, fmt.Errorf("EMC_9FRONT_AUTH_DOM, EMC_9FRONT_AUTH_USER, and EMC_9FRONT_AUTH_PASS must all be set")
	}

	return &nineFrontAuthConfig{
		Domain:   domain,
		User:     user,
		Password: password,
	}, nil
}

func (e EmercoinRPC) endpoint() string {
	if e.Port == "" {
		return e.Location
	}

	parsed, err := url.Parse(e.Location)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		if parsed.Port() != "" {
			return e.Location
		}
		host := parsed.Hostname()
		parsed.Host = net.JoinHostPort(host, e.Port)
		return parsed.String()
	}

	return fmt.Sprintf("%s:%s", e.Location, e.Port)
}

func (e EmercoinRPC) client() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{}
}

func (e EmercoinRPC) Send(ctx context.Context, q Query) (json.RawMessage, error) {
	payload := map[string]any{
		"jsonrpc": "1.0",
		"method":  q.Cmd,
		"params":  q.Args,
		"id":      1,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(e.RPCUser, e.RPCPassword)

	resp, err := e.client().Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rpc http status %s", resp.Status)
	}

	result, err := decodeRPCResponse(respBody)
	if err != nil {
		return nil, err
	}

	return result, nil
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("rpc error %d", e.Code)
	}
	return e.Message
}

func decodeRPCResponse(body []byte) (json.RawMessage, error) {
	var response rpcResponse
	if err := json.Unmarshal(body, &response); err == nil && (response.Error != nil || response.Result != nil) {
		if response.Error != nil {
			return nil, response.Error
		}
		return response.Result, nil
	}

	return json.RawMessage(body), nil
}

type commandExecutor struct {
	backend RPCSend
	timeout time.Duration
}

func (e *commandExecutor) Execute(ctx context.Context, command string) (CommandResult, error) {
	query, err := parseQuery(command)
	if err != nil {
		return CommandResult{Command: strings.TrimSpace(command), Error: err}, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	result, err := e.backend.Send(ctx, query)
	if err != nil {
		return CommandResult{
			Command: query.Cmd,
			Error:   err,
		}, err
	}

	return CommandResult{
		Command: query.Cmd,
		Result:  string(result),
	}, nil
}

func parseQuery(command string) (Query, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Query{}, fmt.Errorf("empty command")
	}

	type jsonCommand struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}

	var jc jsonCommand
	if json.Unmarshal([]byte(command), &jc) == nil && jc.Method != "" {
		return Query{Cmd: jc.Method, Args: jc.Params}, nil
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return Query{}, fmt.Errorf("empty command")
	}

	query := Query{Cmd: fields[0]}
	rest := strings.TrimSpace(strings.TrimPrefix(command, query.Cmd))
	if rest == "" {
		return query, nil
	}

	var list []any
	if strings.HasPrefix(rest, "[") && json.Unmarshal([]byte(rest), &list) == nil {
		query.Args = list
		return query, nil
	}

	var single any
	if (strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "\"")) && json.Unmarshal([]byte(rest), &single) == nil {
		query.Args = []any{single}
		return query, nil
	}

	query.Args = make([]any, 0, len(fields)-1)
	for _, token := range fields[1:] {
		query.Args = append(query.Args, decodeCommandToken(token))
	}

	return query, nil
}

func decodeCommandToken(token string) any {
	var value any
	if json.Unmarshal([]byte(token), &value) == nil {
		return value
	}
	return token
}

type Query struct {
	Cmd  string
	Args []any
}

func (q *Query) Method(cmd string) {
	q.Cmd = cmd
}

func (q *Query) Params(p []any) {
	q.Args = p
}
