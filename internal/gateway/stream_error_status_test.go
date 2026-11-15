package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
)

// Once keepAlive has sent HTTP 200, the error event is the only place
// another magpie can read the failure's status. The Cursor content refusal
// was re-encoded without its 401 and the next hop guessed 502 and retried.
func TestStreamErrorKeepsStatusAcrossProtocols(t *testing.T) {
	for _, proto := range []provider.Protocol{provider.Chat, provider.Anthropic, provider.Responses, provider.Gemini} {
		for _, status := range []int{400, 401, 403, 429, 502} {
			t.Run(fmt.Sprintf("%s/%d", proto, status), func(t *testing.T) {
				w := httptest.NewRecorder()
				streamError(w, proto, "cursor/m", status, "the provider refused this request")
				found := false
				if err := readSSE(strings.NewReader(w.Body.String()), func(_, data string) error {
					e := gjson.Get(data, "error")
					if !e.Exists() {
						e = gjson.Get(data, "response.error")
					}
					if namedStatusOf([]byte(e.Raw)) == status {
						found = true
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if !found {
					t.Fatalf("status %d lost from %s error: %s", status, proto, w.Body)
				}
			})
		}
	}
}

func TestRemoteMagpieStreamedRequestErrorIsNotRetried(t *testing.T) {
	fresh(t)
	pause := retryPause
	retryPause = time.Millisecond
	t.Cleanup(func() { retryPause = pause })
	var asked atomic.Int32
	const message = "Cursor: Unauthorized request.: The provider refused to serve this request based on the content"
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		newSSEWriter(w).comment("keepalive")
		failTo(w, &keptAlive{sent: true, model: "cursor/m"}, provider.Chat, http.StatusBadRequest, message)
	}))
	t.Cleanup(remote.Close)
	if err := provider.Save(provider.Provider{ID: "office", Name: "Office", Preset: provider.RemoteMagpiePreset,
		Key: "synthetic", Chat: remote.URL + "/v1", Models: []string{"cursor/m"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"office/cursor/m","stream":true,"messages":[{"role":"user","content":"hello"}]}`)))
	if n := asked.Load(); n != 1 {
		t.Errorf("a remote request refusal was asked %d times, want 1", n)
	}
	if route := lastRoute(s); route.Status != 400 {
		t.Errorf("remote refusal booked as %d, want 400: %+v", route.Status, route.Tries)
	}
	if !strings.Contains(w.Body.String(), "based on the content") {
		t.Fatalf("lost refusal: %d %s", w.Code, w.Body)
	}
}

// The numeric status is separate from a vendor's string code and type.
func TestTranslatedStreamErrorKeepsStatusAndCode(t *testing.T) {
	const raw = `{"error":{"message":"request refused","status":403,"code":"policy_refusal","type":"permission_error","param":"messages"}}`
	for _, proto := range []provider.Protocol{provider.Chat, provider.Anthropic, provider.Responses} {
		t.Run(string(proto), func(t *testing.T) {
			w := httptest.NewRecorder()
			enc := makeEncoder(proto, newSSEWriter(w), &Request{Model: "m"})
			enc.event(Event{Kind: KError, Text: "request refused", Status: 403, Code: "policy_refusal", Said: json.RawMessage(raw)})
			found := false
			readSSE(strings.NewReader(w.Body.String()), func(_, data string) error {
				e := gjson.Get(data, "error")
				if !e.Exists() {
					e = gjson.Get(data, "response.error")
				}
				if !e.Exists() {
					return nil
				}
				found = true
				if e.Get("status").Int() != 403 || e.Get("code").String() != "policy_refusal" || e.Get("type").String() != "permission_error" || e.Get("param").String() != "messages" {
					t.Errorf("lost error fields: %s", e.Raw)
				}
				return nil
			})
			if !found {
				t.Fatalf("no error: %s", w.Body)
			}
		})
	}
}
