package siicas

import (
	"runtime"
	"strings"
	"testing"
)

func TestNewConfiguredProviderReturnsNilInterfaceWhenDisabled(t *testing.T) {
	provider, err := NewConfiguredProvider(Config{})
	if err != nil {
		t.Fatalf("build disabled provider: %v", err)
	}
	if provider != nil {
		t.Fatalf("disabled provider = %#v, want nil interface", provider)
	}
}

func TestNewConfiguredProviderValidatesScope(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
		want      string
	}{
		{name: "protocol", configure: func(config *Config) { config.Protocol = "easyconnect" }, want: "atrust protocol"},
		{name: "server", configure: func(config *Config) { config.ServerAddress = "vpn.example.edu.cn" }, want: "vpn.sii.edu.cn"},
		{name: "authentication", configure: func(config *Config) { config.AuthType = "auth/psw" }, want: "auth/cas"},
		{name: "login domain", configure: func(config *Config) { config.LoginDomain = "cas.example.edu.cn" }, want: "cas.sii.edu.cn"},
		{name: "static ticket", configure: func(config *Config) { config.StaticTicket = "ST-static" }, want: "static CAS ticket"},
		{name: "credential file pair", configure: func(config *Config) { config.UsernameFile = "/private/username" }, want: "both"},
		{name: "underlay interface", configure: func(config *Config) { config.AutoDetectInterface = false }, want: "underlay interface"},
		{name: "negative health threshold", configure: func(config *Config) { config.HealthFailureThreshold = -1 }, want: "must not be negative"},
		{name: "health interval", configure: func(config *Config) { config.HealthInterval = 0 }, want: "health interval must be positive"},
		{name: "health retry interval", configure: func(config *Config) { config.HealthRetryInterval = 0 }, want: "health retry interval must be positive"},
		{name: "health timeout", configure: func(config *Config) { config.HealthTimeout = 0 }, want: "health timeout must be positive"},
		{name: "health URL", configure: func(config *Config) {
			config.HealthFailureThreshold = 3
			config.KeepAliveURL = ""
		}, want: "HTTP keep-alive URL"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			test.configure(&config)
			_, err := NewConfiguredProvider(config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("provider error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNewConfiguredProviderUsesKeychainByDefault(t *testing.T) {
	provider, err := NewConfiguredProvider(validConfig())
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	if provider == nil {
		t.Fatal("provider is nil")
	}
}

func TestNewConfiguredProviderAcceptsExplicitKernelKeyringSource(t *testing.T) {
	config := validConfig()
	config.CredentialSource = CredentialSourceKernelKeyring

	provider, err := NewConfiguredProvider(config)
	if runtime.GOOS != "linux" {
		if err == nil || !strings.Contains(err.Error(), "only supported on Linux") {
			t.Fatalf("provider error = %v, want unsupported-platform error", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	if provider == nil {
		t.Fatal("provider is nil")
	}
}

func TestNewConfiguredProviderRejectsMixedCredentialSources(t *testing.T) {
	config := validConfig()
	config.CredentialSource = CredentialSourceKernelKeyring
	config.UsernameFile = "/private/username"
	config.PasswordFile = "/private/password"

	_, err := NewConfiguredProvider(config)
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("provider error = %v, want mixed-source error", err)
	}
}

func validConfig() Config {
	return Config{
		Enabled:             true,
		Protocol:            "atrust",
		ServerAddress:       "vpn.sii.edu.cn",
		ServerPort:          443,
		AuthType:            "auth/cas",
		LoginDomain:         "cas.sii.edu.cn",
		KeepAliveURL:        "https://qz.sii.edu.cn/",
		AutoDetectInterface: true,
		HealthInterval:      300,
		HealthRetryInterval: 2,
		HealthTimeout:       3,
	}
}
