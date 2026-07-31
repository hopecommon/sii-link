//go:build !tun

package main

import (
	"context"
	"crypto"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/containers/winquit/pkg/winquit"
	"github.com/hopecommon/sii-link/client"
	atrustclient "github.com/hopecommon/sii-link/client/atrust"
	easyconnectclient "github.com/hopecommon/sii-link/client/easyconnect"
	"github.com/hopecommon/sii-link/configs"
	"github.com/hopecommon/sii-link/dial"
	"github.com/hopecommon/sii-link/internal/hook_func"
	"github.com/hopecommon/sii-link/internal/powerevent"
	"github.com/hopecommon/sii-link/internal/securefile"
	"github.com/hopecommon/sii-link/log"
	"github.com/hopecommon/sii-link/resolve"
	"github.com/hopecommon/sii-link/service"
	"github.com/hopecommon/sii-link/stack"
	"github.com/hopecommon/sii-link/stack/gvisor"
	"github.com/hopecommon/sii-link/stack/tcptunnel"
	"github.com/hopecommon/sii-link/stack/tun"
	"golang.org/x/crypto/pkcs12"
	"inet.af/netaddr"
)

var conf configs.Config

func main() {
	log.Init()
	if conf.LogFile != "" {
		logCloser, err := log.ConfigureFile(conf.LogFile, int64(conf.LogMaxSizeMB)*1024*1024, conf.LogMaxBackups)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Configure rotating log: %s\n", err)
			os.Exit(1)
		}
		defer func() {
			if err := logCloser.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "Close rotating log: %s\n", err)
			}
		}()
	}

	log.Println("Start SII Link " + siiLinkVersionString())
	if conf.DebugDump {
		log.EnableDebug()
	}

	if errs := hook_func.ExecInitialFunc(context.Background(), conf); errs != nil {
		for _, err := range errs {
			log.Printf("Initial SII Link failed: %s", err)
		}
		os.Exit(1)
	}

	var vpnClient client.Client
	switch conf.Protocol {
	case "easyconnect":
		tlsCert := tls.Certificate{}
		if conf.CertFile != "" {
			p12Data, err := os.ReadFile(conf.CertFile)
			if err != nil {
				log.Fatalf("Read certificate file error: %s", err)
			}

			key, cert, err := pkcs12.Decode(p12Data, conf.CertPassword)
			if err != nil {
				log.Fatalf("Decode certificate file error: %s", err)
			}

			tlsCert = tls.Certificate{
				Certificate: [][]byte{cert.Raw},
				PrivateKey:  key.(crypto.PrivateKey),
				Leaf:        cert,
			}
		}

		vpnClient = easyconnectclient.NewClient(
			conf.ServerAddress+":"+fmt.Sprintf("%d", conf.ServerPort),
			conf.Username,
			conf.Password,
			conf.TOTPSecret,
			tlsCert,
			conf.TwfID,
			!conf.DisableMultiLine,
			!conf.DisableServerConfig,
			!conf.SkipDomainResource,
		)

		log.Printf("VPN protocol: %s", conf.Protocol)
		err := vpnClient.(*easyconnectclient.Client).Setup(conf.GraphCodeFile, conf.BindInterface, conf.AutoDetectInterface)
		if err != nil {
			log.Fatalf("VPN client setup error: %s", err)
		}
	case "atrust":
		var err error
		var resourceData []byte

		if conf.ResourceFile != "" {
			resourceData, err = os.ReadFile(conf.ResourceFile)
			if err != nil {
				log.Fatalf("Read resource file error: %s", err)
			}
		}

		var clientData []byte
		if conf.ClientDataFile != "" {
			clientData, err = os.ReadFile(conf.ClientDataFile)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					log.Fatalf("Read client data file error: %s", err)
				}
				log.Println("Client data file does not exist; it will be created after login")
			}
		}

		vpnClient = atrustclient.NewClient(conf.Username, conf.SID, conf.DeviceID, conf.SignKey)
		vpnClient.(*atrustclient.Client).SetSkipTCPTunnelWait(conf.SkipTCPTunnelWait)
		if err := vpnClient.(*atrustclient.Client).SetTCPTunnelPoolSize(conf.TCPTunnelPoolSize); err != nil {
			log.Fatalf("Configure aTrust TCP tunnel pool error: %s", err)
		}
		vpnClient.(*atrustclient.Client).SetVerifyServerTLS(conf.SIIUnattendedCAS)
		casTicketProvider, err := buildSIICASTicketProvider(conf)
		if err != nil {
			log.Fatalf("Configure SII unattended CAS error: %s", err)
		}
		vpnClient.(*atrustclient.Client).SetCASTicketProvider(casTicketProvider)

		log.Printf("VPN protocol: %s", conf.Protocol)
		clientData, err = vpnClient.(*atrustclient.Client).Setup(
			conf.ServerAddress,
			conf.ServerPort,
			conf.Username,
			conf.Password,
			conf.Phone,
			conf.LoginDomain,
			conf.AuthType,
			conf.GraphCodeFile,
			conf.CasTicket,
			conf.OAuth2Code,
			clientData,
			resourceData,
			conf.UpdateBestNodesInterval,
			conf.BindInterface,
			conf.AutoDetectInterface,
		)
		if err != nil {
			log.Fatalf("VPN client setup error: %s", err)
		}

		if conf.ClientDataFile != "" {
			err = securefile.Write(conf.ClientDataFile, clientData)
			if err != nil {
				log.Fatalf("Write client data file error: %s", err)
			}
			log.Printf("Client data saved to %s", conf.ClientDataFile)
		}
	default:
		log.Fatalf("Unsupported VPN protocol: %s", conf.Protocol)
	}

	log.Printf("VPN client started")
	if closer, ok := vpnClient.(interface{ Close() }); ok {
		hook_func.RegisterTerminalFunc("CloseVPNClient", func(ctx context.Context) error {
			closer.Close()
			return nil
		})
	}

	ipResources, err := vpnClient.IPResources()
	if err != nil && !conf.DisableServerConfig {
		log.Println("No IP resources")
	}

	ipSet, err := vpnClient.IPSet()
	if err != nil && !conf.DisableServerConfig {
		log.Println("No IP set")
	}

	domainResources, err := vpnClient.DomainResources()
	if err != nil && !conf.DisableServerConfig {
		log.Println("No domain resources")
	}

	dnsResource, err := vpnClient.DNSResource()
	if err != nil && !conf.DisableServerConfig {
		log.Println("No DNS resource")
	}

	if conf.Protocol == "easyconnect" {
		if !conf.DisableZJUConfig {
			if domainResources == nil {
				domainResources = make(map[string]client.DomainResource)
			}

			domainResources["zju.edu.cn"] = client.DomainResource{
				PortMin:  1,
				PortMax:  65535,
				Protocol: "all",
			}

			if ipResources == nil {
				ipResources = []client.IPResource{}
			}

			ipResources = append([]client.IPResource{{
				IPMin:    net.ParseIP("10.0.0.0"),
				IPMax:    net.ParseIP("10.255.255.255"),
				PortMin:  1,
				PortMax:  65535,
				Protocol: "all",
			}}, ipResources...)

			ipSetBuilder := netaddr.IPSetBuilder{}
			if ipSet != nil {
				ipSetBuilder.AddSet(ipSet)
			}
			ipSetBuilder.AddPrefix(netaddr.MustParseIPPrefix("10.0.0.0/8"))
			ipSet, _ = ipSetBuilder.IPSet()
		}

		for _, customProxyDomain := range conf.CustomProxyDomain {
			if domainResources != nil {
				domainResources[customProxyDomain] = client.DomainResource{
					PortMin:  1,
					PortMax:  65535,
					Protocol: "all",
				}
			} else {
				domainResources = map[string]client.DomainResource{
					customProxyDomain: {
						PortMin:  1,
						PortMax:  65535,
						Protocol: "all",
					},
				}
			}
		}
	}

	var vpnStack stack.Stack
	if conf.TCPTunnelMode {
		vpnStack, err = tcptunnel.NewStack(vpnClient)
		if err != nil {
			log.Fatalf("TCP Tunnel stack setup error: %s", err)
		}
	} else if conf.TUNMode {
		vpnTUNStack, err := tun.NewStack(vpnClient, conf.DNSHijack, conf.FakeIP, ipResources)
		if err != nil {
			log.Fatalf("Tun stack setup error, make sure you are root user : %s", err)
		}

		if conf.AddRoute && ipSet != nil {
			for _, prefix := range ipSet.Prefixes() {
				log.Printf("Add route to %s", prefix.String())
				_ = vpnTUNStack.AddRoute(prefix.String())
			}
		} else if !conf.AddRoute && !conf.DisableZJUConfig && conf.Protocol == "easyconnect" {
			log.Println("Add route to 10.0.0.0/8")
			_ = vpnTUNStack.AddRoute("10.0.0.0/8")
		}

		if conf.FakeIP {
			_ = vpnTUNStack.AddRoute("198.18.0.0/16")
		}

		vpnStack = vpnTUNStack
	} else {
		vpnStack, err = gvisor.NewStack(vpnClient)
		if err != nil {
			log.Fatalf("gVisor stack setup error: %s", err)
		}
	}

	useRemoteDNS := !conf.DisableRemoteDNS
	remoteDNSServer := conf.RemoteDNSServer
	if useRemoteDNS && remoteDNSServer == "auto" {
		remoteDNSServer, err = vpnClient.DNSServer()
		if err != nil {
			useRemoteDNS = false
			remoteDNSServer = "10.10.0.21"
			log.Println("No DNS server provided by server. Disable remote DNS")
		} else {
			log.Printf("Use DNS server %s provided by server", remoteDNSServer)
		}
	}

	vpnResolver := resolve.NewResolver(
		vpnStack,
		remoteDNSServer,
		conf.SecondaryDNSServer,
		conf.DNSTTL,
		domainResources,
		dnsResource,
		useRemoteDNS,
	)
	hook_func.RegisterTerminalFunc("CloseResolver", func(ctx context.Context) error {
		vpnResolver.Close()
		return nil
	})

	for _, customDns := range conf.CustomDNSList {
		ipAddr := net.ParseIP(customDns.IP)
		if ipAddr == nil {
			log.Printf("Custom DNS for host name %s is invalid, SKIP", customDns.HostName)
		}
		vpnResolver.SetPermanentDNS(customDns.HostName, ipAddr)
		log.Printf("Add custom DNS: %s -> %s\n", customDns.HostName, customDns.IP)
	}
	localResolver := service.NewDnsServer(vpnResolver, []string{remoteDNSServer, conf.SecondaryDNSServer})
	vpnStack.SetupResolve(localResolver)
	vpnStack.SetupIPPool(vpnResolver.IPPool)

	go vpnStack.Run()

	if conf.Protocol == "atrust" {
		conf.ProxyAll = false
	}
	vpnDialer := dial.NewDialer(vpnStack, vpnResolver, ipResources, conf.ProxyAll, conf.DialDirectProxy)

	if conf.DNSServerBind != "" {
		go service.ServeDNS(conf.DNSServerBind, localResolver)
	}
	if conf.TUNMode {
		clientIP, _ := vpnClient.IP()
		go service.ServeDNS(clientIP.String()+":53", localResolver)
	}

	if conf.SocksBind != "" {
		go service.ServeSocks5(conf.SocksBind, vpnDialer, vpnResolver, conf.SocksUser, conf.SocksPasswd)
	}

	if conf.HTTPBind != "" {
		go service.ServeHTTP(conf.HTTPBind, vpnDialer)
	}

	if conf.ShadowsocksURL != "" {
		go service.ServeShadowsocks(vpnDialer, conf.ShadowsocksURL)
	}

	for _, portForwarding := range conf.PortForwardingList {
		switch portForwarding.NetworkType {
		case "tcp":
			go service.ServeTCPForwarding(vpnStack, portForwarding.BindAddress, portForwarding.RemoteAddress)
		case "udp":
			go service.ServeUDPForwarding(vpnStack, portForwarding.BindAddress, portForwarding.RemoteAddress)
		default:
			log.Printf("Port forwarding: unknown network type %s. Aborting", portForwarding.NetworkType)
		}
	}

	runtimeFailure := make(chan error, 1)
	var runtimeFailureOnce sync.Once
	reportRuntimeFailure := func(err error) {
		runtimeFailureOnce.Do(func() {
			runtimeFailure <- err
		})
	}
	if !conf.DisableKeepAlive {
		if conf.KeepAliveURL == "" && !useRemoteDNS {
			log.Println("Keep alive is disabled because remote DNS is disabled, and no KeepAliveURL is provided")
		} else {
			keepAliveCtx, keepAliveCancel := context.WithCancel(context.Background())
			hook_func.RegisterTerminalFunc("CloseKeepAlive", func(ctx context.Context) error {
				keepAliveCancel()
				return nil
			})
			failureThreshold := 0
			keepAliveOptions := service.KeepAliveOptions{
				Interval:             60 * time.Second,
				FailureRetryInterval: 60 * time.Second,
				CheckTimeout:         20 * time.Second,
			}
			if conf.SIIUnattendedCAS {
				failureThreshold = conf.SIIHealthFailures
				keepAliveOptions.Interval = time.Duration(conf.SIIHealthInterval) * time.Second
				keepAliveOptions.FailureRetryInterval = time.Duration(conf.SIIHealthRetry) * time.Second
				keepAliveOptions.CheckTimeout = time.Duration(conf.SIIHealthTimeout) * time.Second
				if runtime.GOOS == "darwin" {
					watcher, err := powerevent.Watch(keepAliveCtx)
					if err != nil {
						log.Fatalf("Configure macOS wake-event monitoring error: %s", err)
					}
					wakeTrigger := make(chan struct{}, 1)
					keepAliveOptions.Trigger = wakeTrigger
					go func() {
						for {
							select {
							case <-keepAliveCtx.Done():
								return
							case _, ok := <-watcher.Events:
								if !ok {
									return
								}
								log.Println("macOS wake event detected; running an immediate VPN health check")
								select {
								case wakeTrigger <- struct{}{}:
								default:
								}
							}
						}
					}()
					go func() {
						if err, ok := <-watcher.Done; ok && err != nil {
							if keepAliveCtx.Err() != nil {
								log.Printf("Stop macOS wake-event monitoring: %s", err)
								return
							}
							reportRuntimeFailure(fmt.Errorf("macOS wake-event monitor: %w", err))
						}
					}()
				}
			}
			keepAliveOptions.FailureThreshold = failureThreshold
			go func() {
				err := service.KeepAlive(keepAliveCtx, vpnResolver, vpnDialer, conf.KeepAliveURL, keepAliveOptions)
				if err == nil {
					return
				}
				if failureThreshold > 0 {
					reportRuntimeFailure(fmt.Errorf("VPN health monitor: %w", err))
				} else {
					log.Printf("KeepAlive stopped: %s", err)
				}
			}()
		}
	}

	var runtimeErr error
	if runtime.GOOS == "windows" {
		done := make(chan os.Signal, 1)
		signal.Notify(done, syscall.SIGINT)
		winquit.SimulateSigTermOnQuit(done)
		select {
		case <-done:
		case runtimeErr = <-runtimeFailure:
		}
	} else {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		select {
		case <-quit:
		case runtimeErr = <-runtimeFailure:
		}
	}
	if runtimeErr != nil {
		log.Printf("Runtime failure requires process restart: %s", runtimeErr)
	}
	log.Println("Shutdown SII Link ......")
	if errs := hook_func.ExecTerminalFunc(context.Background()); errs != nil {
		for _, err := range errs {
			log.Printf("Shutdown SII Link failed: %s", err)
		}
	} else {
		log.Println("Shutdown SII Link success, Bye~")
	}
	if runtimeErr != nil {
		os.Exit(1)
	}
}
