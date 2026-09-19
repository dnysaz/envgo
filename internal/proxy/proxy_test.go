package proxy

import "testing"

func TestInjectVars(t *testing.T) {
	vars := MapVars{
		"OPENAI_API_KEY": "sk-secret-123",
		"EMPTY":          "",
	}

	cases := []struct {
		in      string
		want    string
		ok      bool
		missing string
	}{
		{"Bearer {OPENAI_API_KEY}", "Bearer sk-secret-123", true, ""},
		{"{OPENAI_API_KEY}", "sk-secret-123", true, ""},
		{"no placeholders", "no placeholders", true, ""},
		{"a {UNKNOWN} b", "a {UNKNOWN} b", false, "UNKNOWN"},
		{"{EMPTY}x", "x", true, ""},
		{"mixed-{OPENAI_API_KEY}-{OPENAI_API_KEY}", "mixed-sk-secret-123-sk-secret-123", true, ""},
	}
	for i, c := range cases {
		got, ok, missing := InjectVars(c.in, vars)
		if got != c.want || ok != c.ok || missing != c.missing {
			t.Errorf("case %d: InjectVars(%q) = (%q, %v, %q), want (%q, %v, %q)",
				i, c.in, got, ok, missing, c.want, c.ok, c.missing)
		}
	}
}

func TestHostAllowed(t *testing.T) {
	allow := []string{"openai.com", ".googleapis.com"}
	cases := []struct {
		host string
		want bool
	}{
		{"api.openai.com", true},
		{"openai.com", true},
		{"evil.com", false},
		{"api.googleapis.com", true},
		{"maps.googleapis.com", true},
		{"example.com", false},
	}
	for _, c := range cases {
		if got := hostAllowed(c.host, allow); got != c.want {
			t.Errorf("hostAllowed(%q) = %v, want %v", c.host, got, c.want)
		}
	}
	// "*" permits every host
	if !hostAllowed("anything.example", []string{"*"}) {
		t.Error("wildcard allowlist should permit all")
	}
	// empty allow list denies all (SSRF protection)
	if hostAllowed("anything.example", nil) {
		t.Error("empty allowlist should deny all")
	}
	if hostAllowed("anything.example", []string{}) {
		t.Error("empty allowlist should deny all")
	}
}

func TestHostOnly(t *testing.T) {
	cases := map[string]string{
		"https://api.openai.com/v1/chat":     "api.openai.com",
		"https://api.openai.com":             "api.openai.com",
		"https://sub.example.com:8443/x?y=1": "sub.example.com",
	}
	for in, want := range cases {
		if got := hostOnly(in); got != want {
			t.Errorf("hostOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateTargetRejectsHTTP(t *testing.T) {
	h := New(MapVars{}, nil, nil, nil)
	if err := h.validateTarget("http://api.openai.com/"); err == nil {
		t.Fatal("http target must be rejected")
	}
	if err := h.validateTarget("https://evil.example/"); err == nil {
		t.Fatal("https + empty allowlist should be rejected (SSRF protection)")
	}
}
