package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/store"
)

// authKeyForTest builds the minimum key shape the alias policy reads.
func authKeyForTest(aliases []string) store.AuthKey {
	return store.AuthKey{ID: "key-test", Label: "test", Enabled: true, Allowlist: aliases}
}

// SSRF checks protect the one place an operator can make the gateway fetch an
// arbitrary URL, so they are tested directly rather than through the handler.

func TestValidateProviderURLAcceptsRealUpstreams(t *testing.T) {
	for _, url := range []string{
		"https://api.openai.com/v1",
		"http://upstream.internal:8080/v1",
		"https://generativelanguage.googleapis.com/v1beta",
	} {
		if err := ValidateProviderURL(url, false); err != nil {
			t.Errorf("%s should be accepted, got %v", url, err)
		}
	}
}

func TestValidateProviderURLRefusesMetadata(t *testing.T) {
	// The cloud metadata range is refused even when private upstreams are allowed:
	// no legitimate self-hosted provider lives there.
	for _, url := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://169.254.169.254/v1",
		"http://metadata.google.internal/v1",
		"http://100.100.100.200/v1",
	} {
		if err := ValidateProviderURL(url, true); err == nil {
			t.Errorf("%s must be refused even with private upstreams enabled", url)
		}
	}
}

func TestValidateProviderURLPrivatePolicy(t *testing.T) {
	// A self-hosted gateway usually talks to an upstream on its own network, so
	// private addresses are allowed by default and refused when configured so.
	if err := ValidateProviderURL("http://10.0.0.5:8000/v1", true); err != nil {
		t.Errorf("private upstream should be allowed when enabled: %v", err)
	}
	if err := ValidateProviderURL("http://10.0.0.5:8000/v1", false); err == nil {
		t.Error("private upstream must be refused when disabled")
	}
	if err := ValidateProviderURL("http://127.0.0.1:9000/v1", false); err == nil {
		t.Error("loopback must be refused when private upstreams are disabled")
	}
}

func TestValidateProviderURLRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-a-url",
		"ftp://example.com/v1",
		"https://user:pass@example.com/v1",
		"https://",
	}
	for _, url := range cases {
		if err := ValidateProviderURL(url, true); err == nil {
			t.Errorf("%q should be refused", url)
		}
	}
}

func TestJoinPathNormalisesVersionedBase(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com":        "https://api.example.com/v1/chat/completions",
		"https://api.example.com/":       "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1":     "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/":    "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1beta": "https://api.example.com/v1beta/chat/completions",
	}
	for base, want := range cases {
		got, err := chatEndpoint(base)
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
		if got != want {
			t.Errorf("%s: expected %s, got %s", base, want, got)
		}
	}
}

func TestJoinPathRefusesNonHTTP(t *testing.T) {
	if _, err := chatEndpoint("file:///etc/passwd"); err == nil {
		t.Error("a non-http scheme must be refused")
	}
}

func TestRewriteModelPreservesEveryOtherField(t *testing.T) {
	// Parameters this gateway does not model must still reach the provider.
	raw := []byte(`{"model":"fast-chat","messages":[{"role":"user","content":"hi"}],"temperature":0.2,"response_format":{"type":"json_object"}}`)
	got, err := RewriteModel(raw, "gpt-4o-mini")
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["model"] != "gpt-4o-mini" {
		t.Errorf("model was not rewritten: %v", decoded["model"])
	}
	if decoded["temperature"] != 0.2 {
		t.Errorf("temperature was dropped: %v", decoded["temperature"])
	}
	if _, ok := decoded["response_format"]; !ok {
		t.Error("response_format was dropped")
	}
}

func TestRewriteModelRejectsNonObject(t *testing.T) {
	if _, err := RewriteModel([]byte(`"just a string"`), "m"); err == nil {
		t.Error("a non-object body must be refused")
	}
}

func TestPrepareBodyAddsUsageReportingForStreams(t *testing.T) {
	got, err := prepareBody([]byte(`{"model":"fast-chat","stream":true}`), "gpt-4o-mini", true)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	opts, ok := decoded["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Errorf("stream_options.include_usage was not added: %v", decoded["stream_options"])
	}

	// A caller that set its own stream_options keeps them.
	got, err = prepareBody([]byte(`{"model":"fast-chat","stream":true,"stream_options":{"foo":1}}`), "m", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if opts, _ := decoded["stream_options"].(map[string]any); opts["foo"] != float64(1) {
		t.Errorf("the caller's stream_options were replaced: %v", decoded["stream_options"])
	}

	// Non-streaming bodies are left alone.
	got, err = prepareBody([]byte(`{"model":"fast-chat"}`), "m", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "stream_options") {
		t.Error("a non-streaming body must not gain stream_options")
	}
}

func TestBufferResponseReadsUsage(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.WriteString(`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`)
	resp := &http.Response{StatusCode: 200, Body: rec.Result().Body}

	in, out, body, err := bufferResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if in != 11 || out != 7 {
		t.Errorf("expected 11/7 tokens, got %d/%d", in, out)
	}
	if !strings.Contains(string(body), "choices") {
		t.Error("the body was not passed through intact")
	}
}

func TestAllowedAliasPolicy(t *testing.T) {
	// An empty allowlist means every published alias.
	open := authKeyForTest(nil)
	if !allowed(open, "anything") {
		t.Error("an empty allowlist must permit every alias")
	}
	scoped := authKeyForTest([]string{"fast-chat"})
	if !allowed(scoped, "fast-chat") {
		t.Error("the allowlisted alias must be permitted")
	}
	if allowed(scoped, "other") {
		t.Error("a non-allowlisted alias must be refused")
	}
}
