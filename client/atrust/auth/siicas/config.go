package siicas

import (
	"fmt"

	"github.com/hopecommon/sii-link/client/atrust/auth"
)

type Config struct {
	Enabled                bool
	Protocol               string
	ServerAddress          string
	ServerPort             int
	AuthType               string
	LoginDomain            string
	StaticTicket           string
	KeychainAccount        string
	UsernameFile           string
	PasswordFile           string
	ProxyURL               string
	BindInterface          string
	AutoDetectInterface    bool
	KeepAliveDisabled      bool
	KeepAliveURL           string
	HealthFailureThreshold int
	HealthInterval         int
	HealthRetryInterval    int
	HealthTimeout          int
}

func NewConfiguredProvider(config Config) (auth.CASTicketProvider, error) {
	if !config.Enabled {
		return nil, nil
	}
	if config.Protocol != "atrust" {
		return nil, fmt.Errorf("SII unattended CAS requires the atrust protocol")
	}
	if config.ServerAddress != "vpn.sii.edu.cn" || config.ServerPort != 443 {
		return nil, fmt.Errorf("SII unattended CAS only sends credentials through vpn.sii.edu.cn:443")
	}
	if config.AuthType != "auth/cas" {
		return nil, fmt.Errorf("SII unattended CAS requires auth/cas authentication")
	}
	if config.LoginDomain != "cas.sii.edu.cn" {
		return nil, fmt.Errorf("SII unattended CAS requires login domain cas.sii.edu.cn")
	}
	if config.StaticTicket != "" {
		return nil, fmt.Errorf("SII unattended CAS cannot be combined with a static CAS ticket")
	}
	if (config.UsernameFile == "") != (config.PasswordFile == "") {
		return nil, fmt.Errorf("SII unattended CAS requires both username and password files, or neither")
	}
	if config.BindInterface == "" && !config.AutoDetectInterface {
		return nil, fmt.Errorf("SII unattended CAS requires a bound or auto-detected underlay interface")
	}
	if config.HealthFailureThreshold < 0 {
		return nil, fmt.Errorf("SII health failure threshold must not be negative")
	}
	if config.HealthInterval <= 0 {
		return nil, fmt.Errorf("SII health interval must be positive")
	}
	if config.HealthRetryInterval <= 0 {
		return nil, fmt.Errorf("SII health retry interval must be positive")
	}
	if config.HealthTimeout <= 0 {
		return nil, fmt.Errorf("SII health timeout must be positive")
	}
	if config.HealthFailureThreshold > 0 && (config.KeepAliveDisabled || config.KeepAliveURL == "") {
		return nil, fmt.Errorf("SII health-triggered restart requires an HTTP keep-alive URL")
	}

	var credentials CredentialSource
	if config.UsernameFile != "" {
		credentials = FileCredentialSource{
			UsernameFile: config.UsernameFile,
			PasswordFile: config.PasswordFile,
		}
	} else {
		credentials = KeychainCredentialSource{Account: config.KeychainAccount}
	}
	provider, err := NewProvider(credentials, Options{
		CASHost:  config.LoginDomain,
		ProxyURL: config.ProxyURL,
	})
	if err != nil {
		return nil, fmt.Errorf("configure SII unattended CAS: %w", err)
	}
	return provider, nil
}
