package siicas

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hopecommon/sii-link/client/atrust/auth"
	"golang.org/x/net/html"
)

const defaultTimeout = 30 * time.Second

var rsaKeyPattern = regexp.MustCompile(`RSAUtils\.getKeyPair\(\s*["']([0-9a-fA-F]+)["']\s*,\s*["']["']\s*,\s*["']([0-9a-fA-F]+)["']\s*\)`)

type Options struct {
	CASHost    string
	CASPort    string
	ProxyURL   string
	HTTPClient *http.Client
	Timeout    time.Duration
}

type Provider struct {
	credentials CredentialSource
	casHost     string
	casPort     string
	client      http.Client
}

type redirectValidationError struct {
	message string
}

func (e *redirectValidationError) Error() string { return e.message }

func NewProvider(credentials CredentialSource, options Options) (*Provider, error) {
	if credentials == nil {
		return nil, fmt.Errorf("credential source is required")
	}
	casHost := strings.ToLower(strings.TrimSpace(options.CASHost))
	if casHost == "" {
		return nil, fmt.Errorf("CAS host is required")
	}
	if strings.Contains(casHost, ":") {
		return nil, fmt.Errorf("CAS host must not include a port")
	}
	casPort := options.CASPort
	if casPort == "" {
		casPort = "443"
	}
	portNumber, err := strconv.Atoi(casPort)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("CAS port must be between 1 and 65535")
	}
	casPort = strconv.Itoa(portNumber)

	client := http.Client{Timeout: defaultTimeout}
	if options.HTTPClient != nil {
		client = *options.HTTPClient
	}
	if options.Timeout > 0 {
		client.Timeout = options.Timeout
	} else if client.Timeout == 0 {
		client.Timeout = defaultTimeout
	}
	if options.ProxyURL != "" {
		proxyURL, err := url.Parse(options.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse CAS proxy URL: %w", err)
		}
		if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
			return nil, fmt.Errorf("CAS proxy URL must use HTTP or HTTPS")
		}
		if proxyURL.Hostname() == "" {
			return nil, fmt.Errorf("CAS proxy URL must include a host")
		}
		transport, err := cloneTransport(client.Transport)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		client.Transport = transport
	} else if client.Transport == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyFromEnvironment
		client.Transport = transport
	}

	return &Provider{
		credentials: credentials,
		casHost:     casHost,
		casPort:     casPort,
		client:      client,
	}, nil
}

func cloneTransport(roundTripper http.RoundTripper) (*http.Transport, error) {
	if roundTripper == nil {
		return http.DefaultTransport.(*http.Transport).Clone(), nil
	}
	transport, ok := roundTripper.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("custom CAS proxy requires an HTTP transport")
	}
	return transport.Clone(), nil
}

func (p *Provider) Ticket(ctx context.Context, request auth.CASTicketRequest) (string, error) {
	loginURL, err := parseHTTPSURL("login", request.LoginURL)
	if err != nil {
		return "", err
	}
	callbackURL, err := parseHTTPSURL("callback", request.CallbackURL)
	if err != nil {
		return "", err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", fmt.Errorf("create CAS cookie jar: %w", err)
	}
	client := p.client
	client.Jar = jar
	client.CheckRedirect = p.redirectValidator(loginURL, callbackURL)

	pageURL, document, err := p.loadLoginPage(ctx, &client, loginURL)
	if err != nil {
		return "", err
	}
	form, err := parseLoginForm(document, pageURL)
	if err != nil {
		return "", err
	}
	if err := p.validateCASURL("login form", form.action); err != nil {
		return "", err
	}
	if err := p.validateCASURL("login script", form.script); err != nil {
		return "", err
	}

	exponent, modulus, err := p.loadRSAKey(ctx, &client, form.script)
	if err != nil {
		return "", err
	}
	credentials, err := p.credentials.Credentials(ctx)
	if err != nil {
		return "", fmt.Errorf("load SII CAS credentials: %w", err)
	}
	if credentials.Username == "" || credentials.Password == "" {
		return "", fmt.Errorf("SII CAS username and password are required")
	}
	ciphertext, err := encryptPassword(credentials.Password, exponent, modulus)
	if err != nil {
		return "", fmt.Errorf("encrypt SII CAS password: %w", err)
	}

	values := url.Values{
		"username":  {credentials.Username},
		"password":  {ciphertext},
		"execution": {form.execution},
		"encrypted": {"true"},
		"_eventId":  {"submit"},
		"loginType": {"1"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, form.action.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return "", fmt.Errorf("create CAS login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", form.action.Scheme+"://"+form.action.Host)
	req.Header.Set("Referer", pageURL.String())
	req.Header.Set("User-Agent", auth.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		var redirectErr *redirectValidationError
		if errors.As(err, &redirectErr) {
			return "", redirectErr
		}
		return "", fmt.Errorf("submit SII CAS login: %w", err)
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("SII CAS login did not return a ticket redirect (status %d)", resp.StatusCode)
	}
	redirectURL, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("parse CAS ticket redirect: %w", err)
	}
	if err := validateCallbackRedirect(redirectURL, callbackURL); err != nil {
		return "", err
	}
	ticket := redirectURL.Query().Get("ticket")
	if ticket == "" {
		return "", fmt.Errorf("CAS ticket redirect did not contain a ticket")
	}
	return ticket, nil
}

func (p *Provider) redirectValidator(loginURL, callbackURL *url.URL) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme != "https" {
			return &redirectValidationError{message: "CAS redirect must use HTTPS"}
		}
		if req.URL.Query().Get("ticket") != "" {
			if err := validateCallbackRedirect(req.URL, callbackURL); err != nil {
				return &redirectValidationError{message: err.Error()}
			}
			return http.ErrUseLastResponse
		}

		hostname := strings.ToLower(req.URL.Hostname())
		if hostname != strings.ToLower(loginURL.Hostname()) && hostname != p.casHost {
			return &redirectValidationError{message: fmt.Sprintf("unexpected CAS redirect host %q", req.URL.Hostname())}
		}
		return nil
	}
}

func (p *Provider) loadLoginPage(ctx context.Context, client *http.Client, loginURL *url.URL) (*url.URL, *html.Node, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create CAS login-page request: %w", err)
	}
	req.Header.Set("User-Agent", auth.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		var redirectErr *redirectValidationError
		if errors.As(err, &redirectErr) {
			return nil, nil, redirectErr
		}
		return nil, nil, fmt.Errorf("load SII CAS login page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("load SII CAS login page: status %d", resp.StatusCode)
	}
	if err := p.validateCASURL("login page", resp.Request.URL); err != nil {
		return nil, nil, err
	}
	document, err := html.Parse(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("parse SII CAS login page: %w", err)
	}
	return resp.Request.URL, document, nil
}

func (p *Provider) loadRSAKey(ctx context.Context, client *http.Client, scriptURL *url.URL) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scriptURL.String(), nil)
	if err != nil {
		return "", "", fmt.Errorf("create CAS login-script request: %w", err)
	}
	req.Header.Set("User-Agent", auth.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("load SII CAS login script: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("load SII CAS login script: status %d", resp.StatusCode)
	}
	if err := p.validateCASURL("login script response", resp.Request.URL); err != nil {
		return "", "", err
	}
	script, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", "", fmt.Errorf("read SII CAS login script: %w", err)
	}
	matches := rsaKeyPattern.FindSubmatch(script)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("SII CAS login script did not contain an RSA key")
	}
	return string(matches[1]), string(matches[2]), nil
}

func (p *Provider) validateCASURL(label string, target *url.URL) error {
	if target.Scheme != "https" {
		return fmt.Errorf("%s must use HTTPS", label)
	}
	if !strings.EqualFold(target.Hostname(), p.casHost) {
		return fmt.Errorf("%s has unexpected CAS host %q", label, target.Hostname())
	}
	if effectivePort(target) != p.casPort {
		return fmt.Errorf("%s has unexpected CAS port %q", label, effectivePort(target))
	}
	return nil
}

type loginForm struct {
	action    *url.URL
	script    *url.URL
	execution string
}

func parseLoginForm(document *html.Node, pageURL *url.URL) (loginForm, error) {
	formNode := findElement(document, "form", "id", "fm1")
	if formNode == nil {
		return loginForm{}, fmt.Errorf("SII CAS login page did not contain form#fm1")
	}
	actionValue := attribute(formNode, "action")
	if actionValue == "" {
		return loginForm{}, fmt.Errorf("SII CAS login form did not contain an action")
	}
	actionReference, err := url.Parse(actionValue)
	if err != nil {
		return loginForm{}, fmt.Errorf("parse SII CAS login form action: %w", err)
	}
	executionNode := findElement(formNode, "input", "name", "execution")
	execution := attribute(executionNode, "value")
	if execution == "" {
		return loginForm{}, fmt.Errorf("SII CAS login form did not contain an execution token")
	}
	scriptNode := findScript(document)
	if scriptNode == nil {
		return loginForm{}, fmt.Errorf("SII CAS login page did not contain login.js")
	}
	scriptReference, err := url.Parse(attribute(scriptNode, "src"))
	if err != nil {
		return loginForm{}, fmt.Errorf("parse SII CAS login script URL: %w", err)
	}
	return loginForm{
		action:    pageURL.ResolveReference(actionReference),
		script:    pageURL.ResolveReference(scriptReference),
		execution: execution,
	}, nil
}

func findElement(node *html.Node, element, attributeName, attributeValue string) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && node.Data == element && attribute(node, attributeName) == attributeValue {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, element, attributeName, attributeValue); found != nil {
			return found
		}
	}
	return nil
}

func findScript(node *html.Node) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && node.Data == "script" && strings.HasSuffix(attribute(node, "src"), "login.js") {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findScript(child); found != nil {
			return found
		}
	}
	return nil
}

func attribute(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func parseHTTPSURL(label, rawURL string) (*url.URL, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse CAS %s URL: %w", label, err)
	}
	if target.Scheme != "https" {
		return nil, fmt.Errorf("CAS %s URL must use HTTPS", label)
	}
	if target.Hostname() == "" {
		return nil, fmt.Errorf("CAS %s URL must include a host", label)
	}
	return target, nil
}

func validateCallbackRedirect(actual, expected *url.URL) error {
	if actual.Scheme != "https" {
		return fmt.Errorf("CAS ticket redirect must use HTTPS")
	}
	if !strings.EqualFold(actual.Hostname(), expected.Hostname()) {
		return fmt.Errorf("unexpected CAS redirect host %q", actual.Hostname())
	}
	if effectivePort(actual) != effectivePort(expected) {
		return fmt.Errorf("unexpected CAS redirect port %q", actual.Port())
	}
	if actual.Path != expected.Path {
		return fmt.Errorf("unexpected CAS redirect path %q", actual.Path)
	}
	if actual.Query().Get("sfDomain") != expected.Query().Get("sfDomain") {
		return fmt.Errorf("unexpected CAS redirect domain %q", actual.Query().Get("sfDomain"))
	}
	return nil
}

func effectivePort(target *url.URL) string {
	if target.Port() != "" {
		return target.Port()
	}
	if target.Scheme == "https" {
		return "443"
	}
	return "80"
}
