package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
)

// Pi keeps a truncated call's IDs but serializes its unparseable arguments
// as {} and returns a short refusal to execute it. Both gateway hops must
// preserve that correction instead of restoring the remote's old output.
func TestRemoteMagpieKeepsClientOutputContent(t *testing.T) {
	call := func(name, args string) string {
		b, err := json.Marshal(map[string]any{"type": "function_call", "id": "fc_synthetic", "call_id": "call_synthetic", "name": name, "arguments": args})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	const result = `{"type":"function_call_output","call_id":"call_synthetic","output":"Tool call was not executed: the response hit the output token limit. Re-issue with complete arguments."}`
	broken := `{"code":"` + strings.Repeat("x", 4613947-len(`{"code":"`))
	for _, c := range []struct {
		name, output, replay, tail, field, want string
		incomplete, stream, delta               bool
	}{
		{"truncated arguments JSON", call("codemode", broken), call("codemode", "{}"), result, "arguments", "{}", true, false, false},
		{"truncated arguments SSE", call("codemode", broken), call("codemode", "{}"), result, "arguments", "{}", true, true, false},
		{"completed arguments", call("codemode", `{"n":1}`), call("codemode", `{"n":2}`), result, "arguments", `{"n":2}`, false, false, false},
		{"function name", call("old_tool", "{}"), call("new_tool", "{}"), result, "name", "new_tool", false, false, false},
		{"text without IDs", userSaw("old text"), userSaw("edited text"), userAsks("continue"), "content.0.text", "edited text", false, false, false},
		{"unchanged call", call("codemode", "{}"), call("codemode", "{}"), result, "arguments", "{}", false, false, true},
		{"unchanged incomplete call", call("codemode", "{}"), call("codemode", "{}"), result, "arguments", "{}", true, true, true},
		{"call without metadata", `{"type":"function_call","id":"fc_synthetic","status":"completed","call_id":"call_synthetic","name":"codemode","arguments":"{}"}`, `{"arguments":"{}","name":"codemode","call_id":"call_synthetic","type":"function_call"}`, result, "arguments", "{}", false, false, true},
		{"text without metadata", `{"id":"msg_synthetic","status":"completed","type":"message","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]}`, userSaw("answer"), userAsks("continue"), "content.0.text", "answer", false, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			freshRemoteTurns(t)
			vendor, hop := &hopLog{}, &hopLog{}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				vendor.add(r)
				n := vendor.n()
				status, output := "completed", userSaw("done")
				if n == 1 {
					output = c.output
					if c.incomplete {
						status = "incomplete"
					}
				}
				reply := fmt.Sprintf(`{"id":"resp_synthetic%d","object":"response","model":"m1","status":%q,"output":[%s],"usage":{"input_tokens":3,"output_tokens":1}}`, n, status, output)
				if status == "incomplete" {
					reply = strings.TrimSuffix(reply, "}") + `,"incomplete_details":{"reason":"max_output_tokens"}}`
				}
				if c.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: response.%s\ndata: {\"type\":\"response.%s\",\"response\":%s}\n\n", status, status, reply)
				} else {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, reply)
				}
			}))
			t.Cleanup(up.Close)
			if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1"}, Responses: up.URL + "/v1"}); err != nil {
				t.Fatal(err)
			}
			h := New().Handler()
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					b := hop.add(r)
					r.Body = io.NopCloser(strings.NewReader(string(b)))
				}
				h.ServeHTTP(w, r)
			}))
			t.Cleanup(remote.Close)
			if err := provider.Save(provider.Provider{ID: "office", Name: "Office", Preset: provider.RemoteMagpiePreset, Key: "rk", Models: []string{"fake/m1"}, Chat: remote.URL + "/v1", Responses: remote.URL + "/v1", Anthropic: remote.URL}); err != nil {
				t.Fatal(err)
			}
			first := userAsks("synthetic request")
			for i, body := range []string{storeFalseTurn("office/fake/m1", first), storeFalseTurn("office/fake/m1", first, c.replay, c.tail)} {
				if c.stream {
					body = strings.TrimSuffix(body, "}") + `,"stream":true}`
				}
				if rec := askHop(t, h, "pi/1.5.2", body, nil); rec.Code != 200 {
					t.Fatalf("turn %d: status %d", i+1, rec.Code)
				}
			}
			if hop.n() != 2 || vendor.n() != 2 {
				t.Fatalf("asks: hop %d vendor %d", hop.n(), vendor.n())
			}
			got := gjson.Get(vendor.raw(1), "input.1."+c.field).String()
			if got != c.want {
				t.Errorf("vendor lost client %s: got %d bytes, want %d bytes", c.field, len(got), len(c.want))
			}
			if delta := hop.body(1)["previous_response_id"] != nil; delta != c.delta {
				t.Errorf("remote delta = %v, want %v", delta, c.delta)
			}
		})
	}
}

func TestRemoteOutputMatchRequiresKnownContent(t *testing.T) {
	const call = `{"type":"function_call","id":"fc1","call_id":"c1","name":"tool","arguments":"{}"}`
	for _, c := range []struct {
		name, old, next string
		want            bool
	}{
		{"same", call, call, true},
		{"other id", call, strings.Replace(call, "fc1", "fc2", 1), false},
		{"other call id", call, strings.Replace(call, `"c1"`, `"c2"`, 1), false},
		{"missing arguments", call, `{"type":"function_call","id":"fc1","call_id":"c1","name":"tool"}`, false},
		{"invalid output", `{"type":"function_call",`, `{"type":"function_call",`, false},
		{"trailing JSON", call + ` {}`, call + ` {}`, false},
		{"null", `null`, `null`, false},
		{"list", `[]`, `[]`, false},
		{"number precision", `{"type":"custom","value":9007199254740992}`, `{"type":"custom","value":9007199254740993}`, false},
		{"reordered keys", `{"type":"custom","value":{"b":2,"a":1}}`, `{"value":{"a":1,"b":2},"type":"custom"}`, true},
		{"changed phase", `{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"answer"}]}`, userSaw("answer"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := marksMatch([]itemMark{markOf([]byte(c.old))}, []json.RawMessage{json.RawMessage(c.next)}); got != c.want {
				t.Fatalf("reuse cached output = %v, want %v", got, c.want)
			}
		})
	}
}
