package emercoin9p

import "time"

const defaultCommandTimeout = 5 * time.Second

// NamespaceOption configures a namespace.
type NamespaceOption func(*namespaceConfig)

type nineFrontAuthConfig struct {
	Domain   string
	User     string
	Password string
}

type namespaceConfig struct {
	backend        RPCSend
	commandTimeout time.Duration
	serverInfo     *serverInfo
	auth           *nineFrontAuthConfig
}

func defaultNamespaceConfig() namespaceConfig {
	return namespaceConfig{
		commandTimeout: defaultCommandTimeout,
		serverInfo:     newServerInfo(),
	}
}

// WithRPCBackend configures a namespace to execute ctl commands against an RPC backend.
func WithRPCBackend(backend RPCSend) NamespaceOption {
	return func(cfg *namespaceConfig) {
		cfg.backend = backend
	}
}

// WithCommandTimeout configures the timeout used when executing ctl commands.
func WithCommandTimeout(timeout time.Duration) NamespaceOption {
	return func(cfg *namespaceConfig) {
		if timeout > 0 {
			cfg.commandTimeout = timeout
		}
	}
}

// With9FrontAuth configures the server to require 9front dp9ik authentication.
func With9FrontAuth(domain, user, password string) NamespaceOption {
	return func(cfg *namespaceConfig) {
		if domain == "" || user == "" || password == "" {
			cfg.auth = nil
			return
		}
		cfg.auth = &nineFrontAuthConfig{
			Domain:   domain,
			User:     user,
			Password: password,
		}
	}
}

func makeNamespaceConfig(opts []NamespaceOption) namespaceConfig {
	cfg := defaultNamespaceConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
