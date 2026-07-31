package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/hopecommon/sii-link/dial"
	"github.com/hopecommon/sii-link/log"
	"github.com/hopecommon/sii-link/resolve"
)

type KeepAliveOptions struct {
	Interval             time.Duration
	FailureRetryInterval time.Duration
	CheckTimeout         time.Duration
	FailureThreshold     int
	Trigger              <-chan struct{}
}

func KeepAlive(ctx context.Context, resolver *resolve.Resolver, dialer *dial.Dialer, keepAliveURL string, options KeepAliveOptions) error {
	if options.CheckTimeout <= 0 {
		return fmt.Errorf("keep-alive check timeout must be positive")
	}
	check := func(check func(context.Context) error) func(context.Context) error {
		return func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, options.CheckTimeout)
			defer cancel()
			return check(checkCtx)
		}
	}
	if keepAliveURL != "" {
		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, net, addr string) (net.Conn, error) {
					return dialer.Dial(ctx, net, addr)
				},
			},
			Timeout: options.CheckTimeout,
		}
		lastStatus := 0
		return (Monitor{
			Interval:             options.Interval,
			FailureRetryInterval: options.FailureRetryInterval,
			FailureThreshold:     options.FailureThreshold,
			Trigger:              options.Trigger,
			Check: check(func(ctx context.Context) error {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, keepAliveURL, nil)
				if err != nil {
					return fmt.Errorf("create keep-alive request: %w", err)
				}
				resp, err := client.Do(req)
				if err != nil {
					return err
				}
				lastStatus = resp.StatusCode
				return resp.Body.Close()
			}),
			Observe: func(err error) {
				if err == nil {
					log.Printf("KeepAlive: OK, status code %d", lastStatus)
				} else if ctx.Err() == nil {
					log.Printf("KeepAlive: %s", err)
				}
			},
		}).Run(ctx)
	} else {
		remoteUDPResolver, err := resolver.RemoteUDPResolver()
		if err != nil {
			log.Printf("KeepAlive: %s", err)
		}

		remoteTCPResolver, err := resolver.RemoteTCPResolver()
		if err != nil {
			log.Printf("KeepAlive: %s", err)
		}

		if remoteUDPResolver == nil && remoteTCPResolver == nil {
			return fmt.Errorf("keep alive: no remote resolver available")
		}
		return (Monitor{
			Interval:             options.Interval,
			FailureRetryInterval: options.FailureRetryInterval,
			FailureThreshold:     options.FailureThreshold,
			Trigger:              options.Trigger,
			Check: check(func(ctx context.Context) error {
				var lookupErrors []error
				if remoteUDPResolver != nil {
					_, err := remoteUDPResolver.LookupIP(ctx, "ip4", "www.baidu.com")
					if err != nil {
						lookupErrors = append(lookupErrors, fmt.Errorf("UDP: %w", err))
					} else {
						log.Printf("KeepAlive using UDP: OK")
						return nil
					}
				}

				if remoteTCPResolver != nil {
					_, err := remoteTCPResolver.LookupIP(ctx, "ip4", "www.baidu.com")
					if err != nil {
						lookupErrors = append(lookupErrors, fmt.Errorf("TCP: %w", err))
					} else {
						log.Printf("KeepAlive using TCP: OK")
						return nil
					}
				}
				return errors.Join(lookupErrors...)
			}),
			Observe: func(err error) {
				if err != nil && ctx.Err() == nil {
					log.Printf("KeepAlive: %s", err)
				}
			},
		}).Run(ctx)
	}
}
