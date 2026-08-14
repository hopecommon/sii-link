//go:build !tun

package main

import (
	"github.com/hopecommon/sii-link/client/atrust/auth"
	"github.com/hopecommon/sii-link/client/atrust/auth/siicas"
	"github.com/hopecommon/sii-link/configs"
)

func buildSIICASTicketProvider(config configs.Config) (auth.CASTicketProvider, error) {
	return siicas.NewConfiguredProvider(siicas.Config{
		Enabled:                config.SIIUnattendedCAS,
		Protocol:               config.Protocol,
		ServerAddress:          config.ServerAddress,
		ServerPort:             config.ServerPort,
		AuthType:               config.AuthType,
		LoginDomain:            config.LoginDomain,
		StaticTicket:           config.CasTicket,
		CredentialSource:       config.SIICredentialSource,
		KeychainAccount:        config.SIIKeychainAccount,
		UsernameFile:           config.SIIUsernameFile,
		PasswordFile:           config.SIIPasswordFile,
		ProxyURL:               config.SIICASProxy,
		BindInterface:          config.BindInterface,
		AutoDetectInterface:    config.AutoDetectInterface,
		KeepAliveDisabled:      config.DisableKeepAlive,
		KeepAliveURL:           config.KeepAliveURL,
		HealthFailureThreshold: config.SIIHealthFailures,
		HealthInterval:         config.SIIHealthInterval,
		HealthRetryInterval:    config.SIIHealthRetry,
		HealthTimeout:          config.SIIHealthTimeout,
	})
}
