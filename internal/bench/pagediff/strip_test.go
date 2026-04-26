// internal/bench/pagediff/strip_test.go
package pagediff

import (
	"strings"
	"testing"
)

func TestStrip_DropsScriptStyleAndKeepsNav(t *testing.T) {
	in := `<html><head><title>T</title><style>a{}</style></head>
<body><nav>NAVIGATION</nav><script>alert(1)</script>
<p>Hello   world</p><footer>FOOT</footer></body></html>`
	out, err := Strip(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if strings.Contains(out, "alert(1)") {
		t.Errorf("script content leaked: %q", out)
	}
	if strings.Contains(out, "a{}") {
		t.Errorf("style content leaked: %q", out)
	}
	if !strings.Contains(out, "NAVIGATION") {
		t.Errorf("nav text dropped (the naive baseline is supposed to keep it): %q", out)
	}
	if !strings.Contains(out, "FOOT") {
		t.Errorf("footer text dropped: %q", out)
	}
	if !strings.Contains(out, "Hello world") {
		t.Errorf("whitespace not collapsed: %q", out)
	}
}

func TestStrip_HandlesEmptyInput(t *testing.T) {
	out, err := Strip(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestStrip_DropsNoscriptAndIframe(t *testing.T) {
	in := `<noscript>NS</noscript><iframe src=x>IF</iframe><p>P</p>`
	out, err := Strip(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if strings.Contains(out, "NS") || strings.Contains(out, "IF") {
		t.Errorf("noscript/iframe leaked: %q", out)
	}
	if !strings.Contains(out, "P") {
		t.Errorf("paragraph dropped: %q", out)
	}
}
