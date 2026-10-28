package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"github.com/tidwall/gjson"
)

// Claude Code names each session with a side request whose system prompt
// starts "You are naming a coding session" and whose streamed reply is
// {"title":"..."}. That is the title /resume shows. The side request and the
// main turns share the session id inside metadata.user_id, so titles are
// keyed by that id and attached to every row that carries it.
const (
	titlePromptMarker     = "You are naming a coding session"
	webSearchPromptMarker = "You run a web search for another Claude model"
	defaultTitlesFile     = "claude-cache-keepalive.titles.json"
	maxTitles             = 64
	maxTitleRunes         = 42
	titleStreamTTL        = 5 * time.Minute
	// A title request is the first user message plus a fixed prompt. Skip
	// base64 decoding of anything larger at stream start; that is a chat.
	maxTitleRequestB64 = 256 << 10
)

type titleEntry struct {
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

type titleStream struct {
	SessionID string
	Started   time.Time
	Pending   []byte
	Text      strings.Builder
}

var (
	titles       = map[string]titleEntry{}
	titleStreams = map[string]*titleStream{}
	titlesLoaded bool
)

var emptyChunkResponse = []byte(`{"ok":true,"result":{}}`)

// ccSessionID is the Claude Code session id: the header when present, else
// metadata.session_id, else the session_id inside the metadata.user_id JSON
// string (or the legacy "..._session_<uuid>" form).
func ccSessionID(headers http.Header, body []byte) string {
	if sid := claudeSessionID(headers, body); sid != "" {
		return sid
	}
	uid := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if uid == "" {
		return ""
	}
	if gjson.Valid(uid) {
		return strings.TrimSpace(gjson.Get(uid, "session_id").String())
	}
	if i := strings.LastIndex(uid, "_session_"); i >= 0 {
		return strings.TrimSpace(uid[i+len("_session_"):])
	}
	return ""
}

// auxiliaryKind marks Claude Code side requests that are not a conversation:
// session naming and the web search helper.
func auxiliaryKind(body []byte) string {
	sys := systemText(body)
	switch {
	case strings.Contains(sys, titlePromptMarker):
		return "title"
	case strings.Contains(sys, webSearchPromptMarker):
		return "websearch"
	}
	return ""
}

func isBackgroundSession(headers http.Header) bool {
	return strings.EqualFold(headerGetCI(headers, "X-App"), "cli-bg")
}

// handleStreamChunk watches only title-request streams. Every other stream
// returns after reading two fields.
func handleStreamChunk(raw []byte) (out []byte) {
	defer func() {
		if recover() != nil {
			out = emptyChunkResponse
		}
	}()
	head := gjson.GetManyBytes(raw, "RequestID", "ChunkIndex")
	reqID := head[0].String()
	if reqID == "" {
		return emptyChunkResponse
	}
	if head[1].Int() == pluginapi.StreamChunkHeaderInitIndex {
		startTitleStream(reqID, raw)
		return emptyChunkResponse
	}
	mu.Lock()
	ts := titleStreams[reqID]
	mu.Unlock()
	if ts == nil {
		return emptyChunkResponse
	}
	chunk, err := base64.StdEncoding.DecodeString(gjson.GetBytes(raw, "Body").String())
	if err != nil || len(chunk) == 0 {
		return emptyChunkResponse
	}
	done, title := feedTitleStream(reqID, chunk)
	if done && title != "" {
		setSessionTitle(ts.SessionID, title, time.Now())
	}
	return emptyChunkResponse
}

func startTitleStream(reqID string, raw []byte) {
	enc := gjson.GetBytes(raw, "OriginalRequest").String()
	if enc == "" || len(enc) > maxTitleRequestB64 {
		return
	}
	body, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || auxiliaryKind(body) != "title" {
		return
	}
	headers := http.Header{}
	if h := gjson.GetBytes(raw, "RequestHeaders"); h.IsObject() {
		_ = json.Unmarshal([]byte(h.Raw), &headers)
	}
	sid := ccSessionID(headers, body)
	if sid == "" {
		return
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	for id, ts := range titleStreams {
		if now.Sub(ts.Started) > titleStreamTTL {
			delete(titleStreams, id)
		}
	}
	titleStreams[reqID] = &titleStream{SessionID: sid, Started: now}
}

// feedTitleStream appends Anthropic SSE text deltas. It reports done at
// message_stop with the parsed title.
func feedTitleStream(reqID string, chunk []byte) (bool, string) {
	mu.Lock()
	defer mu.Unlock()
	ts := titleStreams[reqID]
	if ts == nil {
		return false, ""
	}
	data := append(ts.Pending, chunk...)
	ts.Pending = nil
	done := false
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		last := i == len(lines)-1
		line = bytes.TrimSpace(line)
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if !bytes.HasPrefix(line, []byte("data:")) || !gjson.ValidBytes(payload) {
			// A chunk may end mid-line; keep the tail for the next chunk.
			if last && len(line) > 0 && len(data) < 1<<20 {
				ts.Pending = append([]byte(nil), lines[i]...)
			}
			continue
		}
		ev := gjson.ParseBytes(payload)
		switch ev.Get("type").String() {
		case "content_block_delta":
			if ev.Get("delta.type").String() == "text_delta" {
				ts.Text.WriteString(ev.Get("delta.text").String())
			}
		case "message_stop":
			done = true
		}
	}
	if !done {
		return false, ""
	}
	delete(titleStreams, reqID)
	return true, parseTitleReply(ts.Text.String())
}

func parseTitleReply(text string) string {
	text = strings.TrimSpace(text)
	// Claude Code asks for a JSON schema; a model may still fence the JSON.
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i && !gjson.Valid(text) {
		text = text[i : j+1]
	}
	if gjson.Valid(text) {
		text = gjson.Get(text, "title").String()
	} else if strings.ContainsAny(text, "{}\n") {
		return ""
	}
	return cleanTitle(text)
}

func cleanTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	title = strings.Trim(title, `"'“”‘’「」`)
	return sanitizeSessionName(truncateRunes(title, maxTitleRunes))
}

func setSessionTitle(sid, title string, at time.Time) {
	title = cleanTitle(title)
	if sid == "" || title == "" {
		return
	}
	mu.Lock()
	titles[sid] = titleEntry{Title: title, At: at}
	pruneTitlesLocked()
	for _, item := range sessions {
		if item == nil || item.CCSessionID != sid || item.CustomLabel {
			continue
		}
		item.Label = title
		item.AITitle = true
	}
	mu.Unlock()
	writePersistedTitles()
}

func titleForLocked(sid string) string {
	if sid == "" {
		return ""
	}
	return titles[sid].Title
}

func pruneTitlesLocked() {
	if len(titles) <= maxTitles {
		return
	}
	ids := make([]string, 0, len(titles))
	for id := range titles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return titles[ids[i]].At.Before(titles[ids[j]].At) })
	for _, id := range ids[:len(ids)-maxTitles] {
		delete(titles, id)
	}
}

func resolvedTitlesPath() string {
	path := resolvedSettingsPath()
	if path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), defaultTitlesFile)
}

func writePersistedTitles() {
	mu.Lock()
	path := resolvedTitlesPath()
	raw, err := json.MarshalIndent(titles, "", "  ")
	mu.Unlock()
	if path == "" || err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o644)
}

func loadPersistedTitles() {
	mu.Lock()
	defer mu.Unlock()
	if titlesLoaded {
		return
	}
	titlesLoaded = true
	path := resolvedTitlesPath()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var saved map[string]titleEntry
	if json.Unmarshal(raw, &saved) != nil {
		return
	}
	for sid, entry := range saved {
		if t := cleanTitle(entry.Title); sid != "" && t != "" {
			titles[sid] = titleEntry{Title: t, At: entry.At}
		}
	}
	pruneTitlesLocked()
}
