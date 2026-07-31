package configs

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestConfigTOMLDecodesSIISettings(t *testing.T) {
	contents := `
sii_unattended_cas = true
sii_keychain_account = "local-user"
sii_username_file = "/private/username"
sii_password_file = "/private/password"
sii_cas_proxy = "http://127.0.0.1:7890"
sii_health_failure_threshold = 4
sii_health_interval = 600
sii_health_retry_interval = 2
sii_health_timeout = 3
tcp_tunnel_pool_size = 3
log_file = "/private/logs/sii-link.log"
log_max_size_mb = 5
log_max_backups = 3
`
	var parsed ConfigTOML
	if _, err := toml.Decode(contents, &parsed); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if parsed.SIIUnattendedCAS == nil || !*parsed.SIIUnattendedCAS {
		t.Fatal("sii_unattended_cas was not decoded")
	}
	if parsed.SIIKeychainAccount == nil || *parsed.SIIKeychainAccount != "local-user" {
		t.Fatal("sii_keychain_account was not decoded")
	}
	if parsed.SIIUsernameFile == nil || *parsed.SIIUsernameFile != "/private/username" {
		t.Fatal("sii_username_file was not decoded")
	}
	if parsed.SIIPasswordFile == nil || *parsed.SIIPasswordFile != "/private/password" {
		t.Fatal("sii_password_file was not decoded")
	}
	if parsed.SIICASProxy == nil || *parsed.SIICASProxy != "http://127.0.0.1:7890" {
		t.Fatal("sii_cas_proxy was not decoded")
	}
	if parsed.SIIHealthFailures == nil || *parsed.SIIHealthFailures != 4 {
		t.Fatal("sii_health_failure_threshold was not decoded")
	}
	if parsed.SIIHealthInterval == nil || *parsed.SIIHealthInterval != 600 {
		t.Fatal("sii_health_interval was not decoded")
	}
	if parsed.SIIHealthRetry == nil || *parsed.SIIHealthRetry != 2 {
		t.Fatal("sii_health_retry_interval was not decoded")
	}
	if parsed.SIIHealthTimeout == nil || *parsed.SIIHealthTimeout != 3 {
		t.Fatal("sii_health_timeout was not decoded")
	}
	if parsed.TCPTunnelPoolSize == nil || *parsed.TCPTunnelPoolSize != 3 {
		t.Fatal("tcp_tunnel_pool_size was not decoded")
	}
	if parsed.LogFile == nil || *parsed.LogFile != "/private/logs/sii-link.log" {
		t.Fatal("log_file was not decoded")
	}
	if parsed.LogMaxSizeMB == nil || *parsed.LogMaxSizeMB != 5 {
		t.Fatal("log_max_size_mb was not decoded")
	}
	if parsed.LogMaxBackups == nil || *parsed.LogMaxBackups != 3 {
		t.Fatal("log_max_backups was not decoded")
	}
}

func TestSIICanaryExampleUsesKnownSettings(t *testing.T) {
	var parsed ConfigTOML
	metadata, err := toml.DecodeFile("sii-canary.toml.example", &parsed)
	if err != nil {
		t.Fatalf("decode SII canary example: %v", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		t.Fatalf("unknown SII canary settings: %v", undecoded)
	}
	if parsed.SIIUnattendedCAS == nil || !*parsed.SIIUnattendedCAS {
		t.Fatal("SII canary example does not enable unattended CAS")
	}
	if parsed.AutoDetectInterface == nil || !*parsed.AutoDetectInterface {
		t.Fatal("SII canary example does not auto-detect the underlay interface")
	}
	if parsed.SkipTCPTunnelWait == nil || !*parsed.SkipTCPTunnelWait {
		t.Fatal("SII canary example does not enable aTrust TCP zero-RTT")
	}
	if parsed.TCPTunnelPoolSize == nil || *parsed.TCPTunnelPoolSize != 3 {
		t.Fatal("SII canary example does not configure the aTrust TCP tunnel pool")
	}
}

func TestSIILocalExampleUsesProductionPorts(t *testing.T) {
	var parsed ConfigTOML
	metadata, err := toml.DecodeFile("sii-local.toml.example", &parsed)
	if err != nil {
		t.Fatalf("decode SII local example: %v", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		t.Fatalf("unknown SII local settings: %v", undecoded)
	}
	if parsed.SocksBind == nil || *parsed.SocksBind != "127.0.0.1:1080" {
		t.Fatal("SII local example does not use the production SOCKS port")
	}
	if parsed.HTTPBind == nil || *parsed.HTTPBind != "127.0.0.1:8888" {
		t.Fatal("SII local example does not use the production HTTP port")
	}
	if parsed.SIIHealthInterval == nil || *parsed.SIIHealthInterval != 300 {
		t.Fatal("SII local example does not use the low-frequency idle health interval")
	}
	if parsed.SkipTCPTunnelWait == nil || !*parsed.SkipTCPTunnelWait {
		t.Fatal("SII local example does not enable aTrust TCP zero-RTT")
	}
	if parsed.TCPTunnelPoolSize == nil || *parsed.TCPTunnelPoolSize != 3 {
		t.Fatal("SII local example does not configure the aTrust TCP tunnel pool")
	}
	if parsed.LogFile == nil || *parsed.LogFile != "/ABSOLUTE/PATH/TO/sii-link.log" {
		t.Fatal("SII local example does not configure the bounded application log")
	}
	if parsed.LogMaxSizeMB == nil || *parsed.LogMaxSizeMB != 5 {
		t.Fatal("SII local example does not cap each log at 5 MiB")
	}
	if parsed.LogMaxBackups == nil || *parsed.LogMaxBackups != 3 {
		t.Fatal("SII local example does not retain exactly three backups")
	}
}
