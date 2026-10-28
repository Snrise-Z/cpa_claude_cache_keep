package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func ccBody(sid, firstUser, system string) []byte {
	uid, _ := json.Marshal(map[string]string{"device_id": "d", "session_id": sid})
	return []byte(fmt.Sprintf(
		`{"model":"claude-opus-5","max_tokens":16000,"messages":[{"role":"user","content":%q}],"system":[{"type":"text","text":%q}],"metadata":{"user_id":%q}}`,
		firstUser, system, string(uid),
	))
}

func titleRequestBody(sid string) []byte {
	return ccBody(sid, "<session>\n阅读交接文档，看第一轮结果\n</session>", "You are naming a coding session so the user can pick it out of a long list of sessions.")
}

func chunkCall(t *testing.T, reqID string, idx int, original, body []byte) {
	t.Helper()
	raw, err := json.Marshal(pluginapi.StreamChunkInterceptRequest{
		RequestID:       reqID,
		RequestHeaders:  http.Header{"X-App": []string{"cli"}},
		OriginalRequest: original,
		Body:            body,
		ChunkIndex:      idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out := handleStreamChunk(raw); string(out) != string(emptyChunkResponse) {
		t.Fatalf("stream chunk must pass through unchanged, got %s", out)
	}
}

func sse(events ...string) []byte {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString("event: x\ndata: " + ev + "\n\n")
	}
	return []byte(b.String())
}

func delta(text string) string {
	raw, _ := json.Marshal(text)
	return `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + string(raw) + `}}`
}

func TestCCSessionIDFromUserIDJSON(t *testing.T) {
	if got := ccSessionID(nil, ccBody("5f02ecce-dcb7", "hi", "sys")); got != "5f02ecce-dcb7" {
		t.Fatalf("session id from metadata.user_id JSON: %q", got)
	}
	legacy := []byte(`{"metadata":{"user_id":"user_abc_account_def_session_1234-5678"}}`)
	if got := ccSessionID(nil, legacy); got != "1234-5678" {
		t.Fatalf("legacy user_id form: %q", got)
	}
	h := http.Header{"X-Claude-Code-Session-Id": []string{"hdr-sid"}}
	if got := ccSessionID(h, ccBody("body-sid", "hi", "sys")); got != "hdr-sid" {
		t.Fatalf("header should win: %q", got)
	}
}

func TestTitleRequestIsNotASession(t *testing.T) {
	resetSessionsForTest()
	t.Cleanup(resetSessionsForTest)
	upsertSession("claude-opus-5", "claude", "claude", nil, titleRequestBody("s1"))
	web := ccBody("s1", "Perform a web search for the query: go 1.27", "You run a web search for another Claude model, which is waiting on the result")
	upsertSession("claude-opus-5", "claude", "claude", nil, web)
	if n := len(listSessions()); n != 0 {
		t.Fatalf("side requests must not become rows, got %d", n)
	}
}

func TestStreamedTitleLabelsSession(t *testing.T) {
	resetSessionsForTest()
	t.Cleanup(resetSessionsForTest)
	upsertSession("claude-opus-5", "claude", "claude", nil, ccBody("s1", "阅读 /data/home/x/交接文档-pku-hpc-20261024.md ，请问已经完整跑过一轮的结果如何", "main"))
	if got := listSessions()[0]; got.AITitle || !strings.HasPrefix(got.Label, "阅读") {
		t.Fatalf("before the title arrives the first message is the label: %+v", got.Label)
	}

	chunkCall(t, "r1", pluginapi.StreamChunkHeaderInitIndex, titleRequestBody("s1"), nil)
	chunkCall(t, "r1", 0, nil, sse(`{"type":"message_start","message":{}}`, delta(`{"title":"pku-`)))
	// A chunk boundary inside an SSE line must not lose text.
	part := sse(delta("hpc 交接文档首轮"), delta(`运行结果"}`))
	chunkCall(t, "r1", 1, nil, part[:20])
	chunkCall(t, "r1", 2, nil, part[20:])
	chunkCall(t, "r1", 3, nil, sse(`{"type":"content_block_stop","index":0}`, `{"type":"message_stop"}`))

	got := listSessions()[0]
	if got.Label != "pku-hpc 交接文档首轮运行结果" || !got.AITitle {
		t.Fatalf("label should be Claude Code's title, got %q ai=%v", got.Label, got.AITitle)
	}
	if len(titleStreams) != 0 {
		t.Fatal("finished title stream should be dropped")
	}

	// New turns keep the title instead of falling back to the first message.
	upsertSession("claude-opus-5", "claude", "claude", nil, ccBody("s1", "阅读 /data/home/x/交接文档-pku-hpc-20261024.md ，请问已经完整跑过一轮的结果如何", "main"))
	if got := listSessions()[0]; got.Label != "pku-hpc 交接文档首轮运行结果" {
		t.Fatalf("title should stick across turns, got %q", got.Label)
	}

	// A custom name wins; clearing it returns to the title.
	id := got.ID
	renameSession(id, "我的名字")
	setSessionTitle("s1", "新标题", time.Now())
	if got := listSessions()[0]; got.Label != "我的名字" {
		t.Fatalf("custom label must win over titles, got %q", got.Label)
	}
	renameSession(id, "")
	if got := listSessions()[0]; got.Label != "新标题" || !got.AITitle {
		t.Fatalf("clearing the name should return to the title, got %q", got.Label)
	}
}

func TestTitleArrivingBeforeFirstTurn(t *testing.T) {
	resetSessionsForTest()
	t.Cleanup(resetSessionsForTest)
	setSessionTitle("s2", "  \"Plugin UI redesign\"  ", time.Now())
	upsertSession("claude-opus-5", "claude", "claude", nil, ccBody("s2", "把 UI 改成米白色", "main"))
	if got := listSessions()[0]; got.Label != "Plugin UI redesign" || !got.AITitle {
		t.Fatalf("a known title should label a new row, got %q", got.Label)
	}
}

func TestStreamChunkIgnoresOtherStreams(t *testing.T) {
	resetSessionsForTest()
	t.Cleanup(resetSessionsForTest)
	chunkCall(t, "r9", pluginapi.StreamChunkHeaderInitIndex, ccBody("s9", "普通对话", "main"), nil)
	if len(titleStreams) != 0 {
		t.Fatal("non-title streams must not be tracked")
	}
	chunkCall(t, "r9", 0, nil, sse(delta("hello")))
	if out := handleStreamChunk([]byte(`not json`)); string(out) != string(emptyChunkResponse) {
		t.Fatal("garbage must still pass through")
	}
}

func TestParseTitleReply(t *testing.T) {
	cases := map[string]string{
		`{"title":"Cache keepalive plugin"}`:      "Cache keepalive plugin",
		`Cache keepalive plugin`:                  "Cache keepalive plugin",
		"```json\n{\"title\":\"CPA 状态页主题\"}\n```": "CPA 状态页主题",
		`{"title":`:          "",
		"line one\nline two": "",
	}
	for in, want := range cases {
		if got := parseTitleReply(in); got != want {
			t.Errorf("parseTitleReply(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTitlesPersistNextToSettings(t *testing.T) {
	resetSessionsForTest()
	t.Cleanup(resetSessionsForTest)
	dir := t.TempDir()
	settingsPath = filepath.Join(dir, "s.json")
	persistSettings = true
	setSessionTitle("s3", "Persisted title", time.Now())
	if _, err := os.Stat(filepath.Join(dir, defaultTitlesFile)); err != nil {
		t.Fatalf("titles file not written: %v", err)
	}
	mu.Lock()
	titles = map[string]titleEntry{}
	titlesLoaded = false
	mu.Unlock()
	loadPersistedTitles()
	mu.Lock()
	got := titleForLocked("s3")
	mu.Unlock()
	if got != "Persisted title" {
		t.Fatalf("title should reload from disk, got %q", got)
	}
}

func TestTitlesAreCapped(t *testing.T) {
	resetSessionsForTest()
	t.Cleanup(resetSessionsForTest)
	base := time.Now()
	for i := 0; i < maxTitles+5; i++ {
		setSessionTitle(fmt.Sprintf("s%d", i), fmt.Sprintf("t%d", i), base.Add(time.Duration(i)*time.Second))
	}
	if len(titles) != maxTitles {
		t.Fatalf("titles=%d", len(titles))
	}
	if _, ok := titles["s0"]; ok {
		t.Fatal("oldest titles should go first")
	}
}

func TestSessionLabelSkipsHarnessBlocks(t *testing.T) {
	first := "<system-reminder>\nAttribution for git commits\n</system-reminder>\n<local-command-caveat>Caveat</local-command-caveat>\n<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>\nclone 这个项目，把它的 UI 改成米白色底"
	if got := sessionLabel(claudeBody(first, "sys")); got != "clone 这个项目，把它的 UI 改成米白色底" {
		t.Fatalf("label=%q", got)
	}
	if got := sessionLabel(claudeBody("<system-reminder>only</system-reminder>", "sys")); got == "" || got == "未命名对话" {
		t.Fatalf("a message that is only a reminder still needs a label, got %q", got)
	}
}
