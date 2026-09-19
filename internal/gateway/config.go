package gateway

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Config is the public-mode manifest. The developer declares every outbound
// endpoint here; the browser can only reference routes by name and can never
// choose a target URL.
type Config struct {
	// TrustProxy makes the gateway read the client IP from X-Forwarded-For.
	// Enable only when envGo sits behind a reverse proxy you control.
	TrustProxy bool `json:"trust_proxy"`
	// DefaultRateLimit applies to routes without their own rate_limit.
	DefaultRateLimit string `json:"default_rate_limit"`
	// ScrubResponse replaces any known secret value found in a non-streaming
	// upstream response body with [REDACTED].
	ScrubResponse bool    `json:"scrub_response"`
	Routes        []Route `json:"routes"`
}

type Route struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	// Target is the fixed upstream URL. Must be https.
	Target string `json:"target"`
	// Vars is the allow-set of env variable names this route may use. A client
	// may reference {NAME} only for names listed here.
	Vars      []string     `json:"vars"`
	Inject    InjectConfig `json:"inject"`
	RateLimit string       `json:"rate_limit"`
	Auth      *AuthConfig  `json:"auth"`
}

type InjectConfig struct {
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
	Body    map[string]any    `json:"body"`
}

type AuthConfig struct {
	// Type is currently only "bearer": the client must send
	// "Authorization: Bearer <value of env var Secret>".
	Type string `json:"type"`
	// Secret is the env variable name holding the expected token.
	Secret string `json:"secret"`
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if len(cfg.Routes) == 0 {
		return nil, fmt.Errorf("config %s has no routes", path)
	}
	seen := map[string]bool{}
	for i := range cfg.Routes {
		if err := validateRoute(&cfg.Routes[i]); err != nil {
			return nil, fmt.Errorf("route %d: %w", i, err)
		}
		name := strings.ToLower(cfg.Routes[i].Name)
		if seen[name] {
			return nil, fmt.Errorf("duplicate route name %q", cfg.Routes[i].Name)
		}
		seen[name] = true
	}
	return &cfg, nil
}

func validateRoute(r *Route) error {
	if r.Name == "" || !namePattern.MatchString(r.Name) {
		return fmt.Errorf("invalid name %q (use letters, digits, - and _)", r.Name)
	}
	if !strings.HasPrefix(r.Target, "https://") {
		return fmt.Errorf("route %q: target must be https", r.Name)
	}
	if r.Method != "" {
		for _, m := range strings.Split(r.Method, ",") {
			if !validMethod(strings.TrimSpace(m)) {
				return fmt.Errorf("route %q: invalid method %q", r.Name, m)
			}
		}
	}
	if r.Auth != nil {
		if r.Auth.Type != "bearer" {
			return fmt.Errorf("route %q: auth.type must be \"bearer\"", r.Name)
		}
		if r.Auth.Secret == "" {
			return fmt.Errorf("route %q: auth.secret is required", r.Name)
		}
	}
	if _, _, err := parseRate(r.RateLimit); err != nil {
		return fmt.Errorf("route %q: %w", r.Name, err)
	}
	if _, err := url.Parse(r.Target); err != nil {
		return fmt.Errorf("route %q: target URL invalid: %w", r.Name, err)
	}
	if err := validateInject(&r.Inject); err != nil {
		return fmt.Errorf("route %q: %w", r.Name, err)
	}
	return nil
}

func validateInject(inj *InjectConfig) error {
	for k, v := range inj.Query {
		if !namePattern.MatchString(k) {
			return fmt.Errorf("invalid query key %q", k)
		}
		if !isValidInjectValue(v) {
			return fmt.Errorf("invalid query value %q for key %q", v, k)
		}
	}
	for k := range inj.Headers {
		if !namePattern.MatchString(k) {
			return fmt.Errorf("invalid header key %q", k)
		}
	}
	for k := range inj.Body {
		if !namePattern.MatchString(k) {
			return fmt.Errorf("invalid body key %q", k)
		}
	}
	return nil
}

func isValidInjectValue(v string) bool {
	if v == "" {
		return true
	}
	parts := strings.Split(v, "{")
	for _, p := range parts[1:] {
		if idx := strings.Index(p, "}"); idx > 0 {
			name := p[:idx]
			if !namePattern.MatchString(name) {
				return false
			}
		}
	}
	return true
}

func validMethod(m string) bool {
	switch strings.ToUpper(m) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	}
	return false
}

// MethodAllowed reports whether the route accepts the given HTTP method.
func (r *Route) MethodAllowed(m string) bool {
	m = strings.ToUpper(m)
	if strings.TrimSpace(r.Method) == "" {
		return m == "POST"
	}
	for _, allowed := range strings.Split(r.Method, ",") {
		if strings.ToUpper(strings.TrimSpace(allowed)) == m {
			return true
		}
	}
	return false
}

// AllowedVars returns the route's variable allow-set as a lookup map.
func (r *Route) AllowedVars() map[string]bool {
	set := make(map[string]bool, len(r.Vars))
	for _, v := range r.Vars {
		set[v] = true
	}
	return set
}
