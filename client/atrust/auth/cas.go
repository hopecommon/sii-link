package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/hopecommon/sii-link/log"
)

type CASTicketRequest struct {
	LoginURL    string
	CallbackURL string
}

// ErrLoginNotSubmitted means no CAS callback has reached the SII gateway.
var ErrLoginNotSubmitted = errors.New("SII login has not been submitted")

type CASTicketProvider interface {
	Ticket(context.Context, CASTicketRequest) (string, error)
}

type CASTicketProviderFunc func(context.Context, CASTicketRequest) (string, error)

func (f CASTicketProviderFunc) Ticket(ctx context.Context, request CASTicketRequest) (string, error) {
	return f(ctx, request)
}

type CASLogin struct {
	Domain         string
	Ticket         string
	TicketProvider CASTicketProvider
}

func (m CASLogin) AuthType() string {
	return "auth/cas"
}

func (m CASLogin) LoginDomain() string {
	return m.Domain
}

func (m CASLogin) login(s *Session, authInfo AuthInfo) error {
	return s.loginAuthCas(authInfo.LoginURL, m.Domain, m.Ticket, m.TicketProvider)
}

func (s *Session) loginAuthCas(loginURL, loginDomain, ticket string, provider CASTicketProvider) error {
	callback := s.casCallbackFromTicket(loginDomain, ticket)
	if ticket == "" {
		resolvedLoginURL, err := s.resolveURL(loginURL)
		if err != nil {
			return fmt.Errorf("resolve CAS login URL: %w", err)
		}

		if provider != nil {
			ticket, err = provider.Ticket(context.Background(), CASTicketRequest{
				LoginURL:    resolvedLoginURL,
				CallbackURL: s.casCallbackURL(loginDomain),
			})
			if err != nil {
				return errors.Join(ErrLoginNotSubmitted, fmt.Errorf("get CAS ticket: %w", err))
			}
			if ticket == "" {
				return errors.Join(ErrLoginNotSubmitted, fmt.Errorf("CAS ticket provider returned an empty ticket"))
			}
			callback = s.casCallbackFromTicket(loginDomain, ticket)
		} else {
			callback, err = s.interactiveCas(resolvedLoginURL)
			if err != nil {
				return err
			}
		}
	}

	if err := s.cas(callback); err != nil {
		return err
	}
	_, _, err := s.authConfig(true, false)
	return err
}

func (s *Session) casCallbackURL(loginDomain string) string {
	params := url.Values{
		"sfDomain": {loginDomain},
	}
	return s.baseURL + "/passport/v1/auth/cas?" + params.Encode()
}

func (s *Session) casCallbackFromTicket(loginDomain, ticket string) string {
	params := url.Values{
		"sfDomain": {loginDomain},
		"ticket":   {ticket},
	}
	return s.baseURL + "/passport/v1/auth/cas?" + params.Encode()
}

func (s *Session) resolveURL(rawURL string) (string, error) {
	baseURL, err := url.Parse(s.baseURL)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(reference).String(), nil
}

func (s *Session) interactiveCas(loginURL string) (string, error) {
	log.Printf("Visit %s to login, and catch the callback url", loginURL)
	log.Println("Please enter the callback url:")
	var callback string
	_, err := fmt.Scanln(&callback)
	if err != nil {
		return "", err
	}

	callbackURL, err := url.Parse(callback)
	if err != nil {
		return "", err
	}
	if err := validateCASCallbackURL(callbackURL, s.baseHost); err != nil {
		return "", err
	}
	return callback, nil
}

func validateCASCallbackURL(callbackURL *url.URL, baseHost string) error {
	if callbackURL.Scheme != "https" {
		return fmt.Errorf("invalid callback url: scheme not https")
	}
	if callbackURL.Host != baseHost {
		return fmt.Errorf("invalid callback url: host not match")
	}
	if callbackURL.Path != "/passport/v1/auth/cas" {
		return fmt.Errorf("invalid callback url: path not match")
	}
	queries := callbackURL.Query()
	if queries.Get("ticket") == "" {
		return fmt.Errorf("invalid callback url: ticket not found")
	}
	return nil
}

func (s *Session) cas(callback string) error {
	log.Println("Perform GET /passport/v1/auth/cas")

	req, _ := http.NewRequest("GET", callback, nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("x-csrf-token", s.csrfToken)
	req.Header.Set("x-sdp-traceid", s.randSdpId())

	prevCheckRedirect := s.client.CheckRedirect
	s.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	defer func() { s.client.CheckRedirect = prevCheckRedirect }()

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if resp.StatusCode != 302 {
		return fmt.Errorf("invalid status code: %d", resp.StatusCode)
	}
	ticket, err := parsePortalTicketFromRedirect(resp.Header.Get("Location"), s.baseHost)
	if err != nil {
		return err
	}

	body, _ := io.ReadAll(resp.Body)
	log.DebugPrintf("Received cas data: %s", string(body))
	s.ticket = ticket
	return nil
}
