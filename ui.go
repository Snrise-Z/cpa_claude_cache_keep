package main

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"
)

type statusView struct {
	State          string
	StateLabel     string
	StateHint      string
	IntervalMin    int
	MaxTokens      int
	MaxSessions    int
	IdleEvictMin   int
	SessionCount   int
	EnabledCount   int
	BackgroundCnt  int
	HasSessions    bool
	Sessions       []sessionRow
	LastPing       string
	LastPingAgo    string
	NextPing       string
	NextPingAgo    string
	PingError      string
	Now            string
	Version        string
	BudgetWindow   string
	BudgetChat     string
	BudgetKeep     string
	BudgetCap      string
	BudgetRemain   string
	BudgetUsed     string
	BudgetBlock    string
	BudgetNote     string
	ReservePercent int
	StopPercent    int
	ChatBlocked    bool
	GuardChat      bool
	GuardHref      string
	KeepPaused     bool
	HasReset       bool
	UsedKnown      bool
	BudgetUsed7d   string
	Bar5h          int
	Tone5h         string
	Bar7d          int
	Tone7d         string
	HasReset7d     bool
	Used7dKnown    bool
	Reset7dAt      string
	Reset7dIn      string
	ResetAt        string
	ResetIn        string
	HasQuotaLog    bool
	QuotaLog       []quotaRow
	SubagentNote   string
}

type quotaRow struct {
	At       string
	Ago      string
	Kind     string
	Model    string
	HeaderOK bool
	Util5h   string
	Status5h string
	Reset5h  string
	Util7d   string
	Status7d string
	Unified  string
	Raw      string
	Cache    string
	RowClass string
}

type sessionRow struct {
	ID          string
	Label       string
	TitleSource string
	TitleTag    string
	Background  bool
	Model       string
	Enabled     bool
	ToggleHref  string
	ForgetHref  string
	SavedAt     string
	SavedAgo    string
	LastPing    string
	LastPingAgo string
	NextPing    string
	NextPingAgo string
	PingError   string
	CacheTTL    string
	BodySize    string
	Messages    string
	PingCache   string
	PingHref    string
	PingPaused  bool
	RowClass    string
}

func renderStatusHTML() string {
	st := currentStatus()
	view := buildStatusView(st)
	var buf bytes.Buffer
	if err := statusTemplate.Execute(&buf, view); err != nil {
		return `<!doctype html><meta charset="utf-8"><title>缓存保活</title><p>页面渲染失败</p>`
	}
	return buf.String()
}

func buildStatusView(st statusPage) statusView {
	view := statusView{
		IntervalMin:    st.IntervalMin,
		MaxTokens:      st.MaxTokens,
		MaxSessions:    st.MaxSessions,
		IdleEvictMin:   st.IdleEvictMin,
		SessionCount:   len(st.Sessions),
		EnabledCount:   st.EnabledCount,
		HasSessions:    len(st.Sessions) > 0,
		LastPing:       formatTime(st.LastPingAt),
		LastPingAgo:    formatAgo(st.Now, st.LastPingAt),
		NextPing:       formatTime(st.NextPingAt),
		NextPingAgo:    formatUntil(st.Now, st.NextPingAt),
		PingError:      st.LastPingError,
		Now:            st.Now.In(beijingZone).Format("15:04:05"),
		BudgetWindow:   fmt.Sprintf("%d 小时", (st.Budget.WindowMin+59)/60),
		BudgetChat:     formatUnits(st.Budget.ChatUnits),
		BudgetKeep:     formatUnits(st.Budget.KeepaliveUnits),
		BudgetCap:      formatUnits(st.Budget.KeepaliveCap),
		BudgetRemain:   formatUnits(st.Budget.KeepaliveRemain),
		BudgetUsed:     formatPercent(st.Budget.UsedPercent, st.Budget.UsedKnown),
		BudgetBlock:    fmt.Sprintf("%d%%", st.Budget.BlockAtPercent),
		ReservePercent: st.Budget.ReservePercent,
		StopPercent:    stopPercentFromReserve(st.Budget.ReservePercent),
		ChatBlocked:    st.Budget.ChatBlocked,
		GuardChat:      st.Budget.GuardChat,
		GuardHref:      guardHref(!st.Budget.GuardChat),
		KeepPaused:     st.Budget.KeepalivePaused,
		HasReset:       !st.Budget.ResetAt.IsZero(),
		UsedKnown:      st.Budget.UsedKnown,
		BudgetUsed7d:   formatPercent(st.Budget.Used7dPercent, st.Budget.Used7dKnown),
		HasReset7d:     !st.Budget.Reset7d.IsZero(),
		Used7dKnown:    st.Budget.Used7dKnown,
		Reset7dAt:      formatTime(st.Budget.Reset7d),
		Reset7dIn:      formatUntil(st.Now, st.Budget.Reset7d),
		ResetAt:        formatTime(st.Budget.ResetAt),
		ResetIn:        formatUntil(st.Now, st.Budget.ResetAt),
		Version:        st.Version,
		HasQuotaLog:    len(st.QuotaLog) > 0,
		Bar5h:          meterWidth(st.Budget.UsedPercent, st.Budget.UsedKnown),
		Tone5h:         meterTone(st.Budget.UsedPercent, st.Budget.UsedKnown, float64(st.Budget.BlockAtPercent)),
		Bar7d:          meterWidth(st.Budget.Used7dPercent, st.Budget.Used7dKnown),
		Tone7d:         meterTone(st.Budget.Used7dPercent, st.Budget.Used7dKnown, 95),
	}
	if st.SubagentSkipped > 0 {
		view.SubagentNote = fmt.Sprintf("已跳过 %d 次子代理请求，不进保活名单。最近：%s %s（%s）", st.SubagentSkipped, dash(st.LastSubagentKind), st.LastSubagentLabel, formatAgo(st.Now, st.LastSubagentAt))
	}
	view.QuotaLog = make([]quotaRow, 0, len(st.QuotaLog))
	for _, sample := range st.QuotaLog {
		view.QuotaLog = append(view.QuotaLog, buildQuotaRow(st.Now, sample))
	}
	switch {
	case st.Budget.ChatBlocked:
		if !st.Budget.ResetAt.IsZero() {
			view.BudgetNote = fmt.Sprintf("CPA 对话已在 %d%% 停住，等到额度刷新（%s，%s）或用量掉回停线。最后 %d%% 留给保活。/compact 仍放行。", st.Budget.BlockAtPercent, formatTime(st.Budget.ResetAt), formatUntil(st.Now, st.Budget.ResetAt), st.Budget.ReservePercent)
		} else {
			view.BudgetNote = fmt.Sprintf("CPA 对话已在 %d%% 停住，等到用量掉回停线或读到刷新时间。最后 %d%% 留给保活。/compact 仍放行。", st.Budget.BlockAtPercent, st.Budget.ReservePercent)
		}
	case !st.Budget.GuardChat:
		view.BudgetNote = "对话拦截已关，到停线也不会本地 429。5 小时额度仍由上游计。"
		view.BudgetBlock = "关闭"
	case st.Budget.KeepalivePaused:
		view.BudgetNote = st.Budget.PauseReason
	case st.Budget.UsedKnown:
		view.BudgetNote = fmt.Sprintf("上游 5 小时已用 %.0f%%。用量真到 %d%% 才拦对话，不按下一发估。最后 %d%% 留给保活。", st.Budget.UsedPercent, st.Budget.BlockAtPercent, st.Budget.ReservePercent)
	case st.Budget.Budget > 0:
		view.BudgetNote = fmt.Sprintf("还没读到上游 5h 用量头，先按加权预算拦对话，预留 %d%% 给保活。", st.Budget.ReservePercent)
	default:
		view.BudgetNote = fmt.Sprintf("等下一轮 CPA 请求带上 5h 用量头后，用量真到 %d%% 才拦对话，把最后 %d%% 留给保活。", st.Budget.BlockAtPercent, st.Budget.ReservePercent)
	}
	interval := time.Duration(st.IntervalMin) * time.Minute
	view.Sessions = make([]sessionRow, 0, len(st.Sessions))
	for _, item := range st.Sessions {
		next := sessionNextPingAt(item.LastSeen, item.LastPingAt, st.Now, interval)
		row := sessionRow{
			ID:          item.ID,
			Label:       item.Label,
			Background:  item.Background,
			Model:       dash(item.Model),
			Enabled:     item.Enabled,
			ToggleHref:  toggleHref(item.ID, !item.Enabled),
			ForgetHref:  "?forget=" + item.ID,
			PingHref:    "?ping=" + item.ID,
			SavedAt:     formatTime(item.LastSeen),
			SavedAgo:    formatAgo(st.Now, item.LastSeen),
			LastPing:    formatTime(item.LastPingAt),
			LastPingAgo: formatAgo(st.Now, item.LastPingAt),
			PingError:   item.LastPingError,
			CacheTTL:    dash(item.Info.CacheTTL),
			BodySize:    formatBytes(len(item.Body)),
			Messages:    dashInt(item.Info.MessageCount),
			PingCache:   cacheVerdict(item.LastPingCacheRead, item.LastPingCacheWrite),
			PingPaused:  item.PingExpensive,
			RowClass:    "off",
		}
		if item.Enabled {
			if item.PingExpensive {
				row.NextPing = "已暂停"
				row.NextPingAgo = "连续未命中，避免反复重写"
			} else {
				row.NextPing = formatTime(next)
				row.NextPingAgo = formatUntil(st.Now, next)
			}
		} else {
			row.NextPing = "—"
		}
		switch {
		case item.LastPingError != "":
			row.RowClass = "error"
		case item.Enabled:
			row.RowClass = "on"
		}
		switch {
		case item.CustomLabel:
			row.TitleSource, row.TitleTag = "custom", "自定义名称"
		case item.AITitle:
			row.TitleSource, row.TitleTag = "ai", "Claude 标题"
		default:
			row.TitleSource, row.TitleTag = "prompt", "首条消息"
		}
		if item.Background {
			view.BackgroundCnt++
		}
		view.Sessions = append(view.Sessions, row)
	}
	switch {
	case st.Budget.ChatBlocked:
		view.State = "armed"
		view.StateLabel = "对话已限流"
		if !st.Budget.ResetAt.IsZero() {
			view.StateHint = fmt.Sprintf("5 小时额度过了停线。对话会一直拦到刷新（%s）或用量掉回停线。/compact 仍可走。", formatTime(st.Budget.ResetAt))
		} else {
			view.StateHint = "5 小时额度过了停线。对话会一直拦到用量掉回停线，或读到刷新时间。/compact 仍可走。"
		}
	case st.Budget.KeepalivePaused && st.EnabledCount > 0:
		view.State = "armed"
		view.StateLabel = "保活暂停"
		view.StateHint = st.Budget.PauseReason
	case st.LastPingError != "" && st.EnabledCount > 0:
		view.State = "error"
		view.StateLabel = "保活失败"
		view.StateHint = "上一轮重放有会话出错。可先取消勾选出问题的对话，或看每条下面的错误。"
	case len(st.Sessions) == 0:
		view.State = "empty"
		view.StateLabel = "等待对话"
		view.StateHint = "还没有记下 Claude 对话。用 Claude Code 走 CPA 正常聊一轮后，这里会出现会话；新对话默认不保活。"
	case st.EnabledCount == 0:
		view.State = "empty"
		view.StateLabel = "未开保活"
		view.StateHint = "会话已经记下，但默认不保活。点左侧方块才会定时续缓存。满员时优先顶掉没开保活的；没开的超过空闲时间也会被丢掉。"
	case st.LastPingAt.IsZero():
		view.State = "armed"
		view.StateLabel = "已记录，等待保活"
		view.StateHint = fmt.Sprintf("当前 %d 路勾选保活。每路从最后一次对话请求起算，每隔 %d 分钟重放一次，并把 max_tokens 卡成 %d。新消息会把该路倒计时重置。", st.EnabledCount, st.IntervalMin, st.MaxTokens)
	default:
		view.State = "ok"
		view.StateLabel = "保活运行中"
		view.StateHint = fmt.Sprintf("已勾选 %d 路。下次保活按各会话最后一次请求对齐，间隔 %d 分钟，输出上限 max_tokens=%d。", st.EnabledCount, st.IntervalMin, st.MaxTokens)
	}
	return view
}

func meterWidth(pct float64, known bool) int {
	if !known || pct <= 0 {
		return 0
	}
	if pct >= 100 {
		return 100
	}
	return int(pct + 0.5)
}

func meterTone(pct float64, known bool, stop float64) string {
	switch {
	case !known:
		return "idle"
	case pct >= stop:
		return "err"
	case pct >= stop-15:
		return "warn"
	}
	return "ok"
}

func toggleHref(id string, enable bool) string {
	on := "0"
	if enable {
		on = "1"
	}
	return "?toggle=" + id + "&on=" + on
}

func guardHref(enable bool) string {
	if enable {
		return "?guard=1"
	}
	return "?guard=0"
}

func buildQuotaRow(now time.Time, sample quotaSample) quotaRow {
	row := quotaRow{
		At:       formatTime(sample.At),
		Ago:      formatAgo(now, sample.At),
		Model:    dash(sample.Model),
		HeaderOK: sample.HeaderOK,
		Util5h:   "未读到",
		Status5h: "—",
		Reset5h:  dash(formatTime(sample.Reset5h)),
		Util7d:   "—",
		Status7d: dash(sample.Status7d),
		Unified:  dash(sample.UnifiedStatus),
		RowClass: "miss",
	}
	switch {
	case sample.Failed:
		row.Kind = "失败"
		row.RowClass = "fail"
	case sample.Keepalive:
		row.Kind = "保活"
	default:
		row.Kind = "对话"
	}
	if sample.HeaderOK {
		row.Util5h = fmt.Sprintf("%.1f%%", sample.Util5h*100)
		row.Status5h = dash(sample.Status5h)
		row.RowClass = "ok"
		if sample.Failed || sample.Status5h == "rejected" {
			row.RowClass = "fail"
		}
	}
	if sample.Header7dOK {
		row.Util7d = fmt.Sprintf("%.1f%%", sample.Util7d*100)
	}
	var raw []string
	if sample.RawUtil5h != "" {
		raw = append(raw, "5h-utilization="+sample.RawUtil5h)
	}
	if sample.RawStatus5h != "" {
		raw = append(raw, "5h-status="+sample.RawStatus5h)
	}
	if sample.RawReset5h != "" {
		raw = append(raw, "5h-reset="+sample.RawReset5h)
	}
	if sample.RawUtil7d != "" {
		raw = append(raw, "7d-utilization="+sample.RawUtil7d)
	}
	if sample.RawStatus7d != "" {
		raw = append(raw, "7d-status="+sample.RawStatus7d)
	}
	if sample.RawReset7d != "" {
		raw = append(raw, "7d-reset="+sample.RawReset7d)
	}
	if sample.UnifiedStatus != "" {
		raw = append(raw, "unified="+sample.UnifiedStatus)
	}
	if len(raw) == 0 {
		row.Raw = "响应头里没有 Anthropic-Ratelimit-Unified-5h-*"
	} else {
		row.Raw = strings.Join(raw, " · ")
	}
	row.Cache = cacheVerdict(sample.CacheRead, sample.CacheWrite)
	return row
}

func dash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

func dashInt(v int) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d", v)
}

func formatPercent(v float64, known bool) string {
	if !known {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", v)
}

func formatUnits(n int64) string {
	if n <= 0 {
		return "0"
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.2fM", float64(n)/1000000)
}

func formatBytes(n int) string {
	if n <= 0 {
		return "—"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2f MB", float64(n)/(1024*1024))
}

var beijingZone = time.FixedZone("CST", 8*3600)

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(beijingZone).Format("2006-01-02 15:04:05") + " 北京时间"
}

func formatAgo(now, then time.Time) string {
	if then.IsZero() {
		return ""
	}
	d := now.Sub(then)
	if d < 0 {
		d = 0
	}
	return humanDuration(d) + "前"
}

func formatUntil(now, then time.Time) string {
	if then.IsZero() {
		return ""
	}
	d := then.Sub(now)
	if d <= 0 {
		return "即将"
	}
	return humanDuration(d) + "后"
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		sec := int(d.Seconds())
		if sec < 1 {
			sec = 1
		}
		return fmt.Sprintf("%d 秒", sec)
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%d 小时", h)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", h, m)
}

var statusTemplate = template.Must(template.New("status").Parse(statusHTML))

const statusHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<title>缓存保活</title>
<style>
:root {
  --bg: #fcfaf6;
  --bg-deep: #f2ede4;
  --card: #fffefc;
  --card-soft: #faf7f1;
  --line: #ede7dc;
  --line-strong: #ddd5c5;
  --text: #2e2a24;
  --text-2: #5d564b;
  --muted: #958b7b;
  --ink: #3c3a35;
  --accent: #b5683f;
  --accent-soft: #f6e7dc;
  --ok: #4c7a5a;
  --ok-soft: #e5efe5;
  --armed: #49698a;
  --armed-soft: #e6edf3;
  --warn: #a7761e;
  --warn-soft: #f7eed8;
  --err: #b0493a;
  --err-soft: #f8e6e1;
  --idle: #a69c8b;
  --idle-soft: #efe9dd;
  --radius: 16px;
  --shadow: 0 1px 1px rgba(76, 60, 36, .04), 0 8px 24px -14px rgba(76, 60, 36, .22);
  --shadow-lift: 0 1px 1px rgba(76, 60, 36, .05), 0 14px 32px -16px rgba(76, 60, 36, .3);
  --serif: "Iowan Old Style", "Palatino Linotype", Palatino, "Songti SC", "Noto Serif CJK SC", "Source Han Serif SC", "STSong", Georgia, serif;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Noto Sans CJK SC", "Microsoft YaHei", system-ui, sans-serif;
  --mono: ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, monospace;
}
* { box-sizing: border-box; }
html { background: var(--bg); color-scheme: light; }
body {
  margin: 0;
  color: var(--text);
  background:
    radial-gradient(1100px 520px at -5% -12%, #fffefb 0%, transparent 62%),
    radial-gradient(900px 480px at 105% -8%, #f7f2e9 0%, transparent 58%),
    var(--bg);
  background-attachment: fixed;
  font: 14.5px/1.6 var(--sans);
  -webkit-font-smoothing: antialiased;
  text-rendering: optimizeLegibility;
}
main { max-width: 1040px; margin: 0 auto; padding: 40px 24px 56px; }
a { color: inherit; }

/* header */
.top { display: flex; gap: 20px; justify-content: space-between; align-items: flex-start; flex-wrap: wrap; margin-bottom: 28px; }
.eyebrow { display: inline-flex; align-items: center; gap: 8px; color: var(--muted); font-size: 12px; letter-spacing: .08em; margin-bottom: 10px; }
.eyebrow .mark { width: 18px; height: 18px; border-radius: 6px; background: linear-gradient(145deg, #d98a5f, var(--accent)); box-shadow: inset 0 0 0 1px rgba(255,255,255,.35); position: relative; }
.eyebrow .mark::after { content: ""; position: absolute; inset: 5px; border-radius: 50%; border: 1.5px solid rgba(255,253,249,.92); }
h1 { margin: 0 0 8px; font: 600 30px/1.2 var(--serif); letter-spacing: -.01em; color: var(--ink); }
.lead { margin: 0; color: var(--text-2); max-width: 44rem; }
.badge {
  display: inline-flex; align-items: center; gap: 9px;
  padding: 8px 14px 8px 12px; border-radius: 999px;
  font-weight: 600; font-size: 13px; white-space: nowrap;
  border: 1px solid color-mix(in srgb, currentColor 22%, transparent);
  background: var(--card); box-shadow: var(--shadow);
}
.badge .dot { width: 8px; height: 8px; border-radius: 50%; background: currentColor; box-shadow: 0 0 0 4px color-mix(in srgb, currentColor 16%, transparent); }
.state-ok { color: var(--ok); }
.state-armed { color: var(--armed); }
.state-empty { color: var(--idle); }
.state-error { color: var(--err); }
.state-ok .dot { animation: breathe 2.6s ease-in-out infinite; }
@keyframes breathe { 50% { box-shadow: 0 0 0 7px color-mix(in srgb, currentColor 6%, transparent); } }

/* stat cards */
.grid { display: grid; gap: 14px; margin-bottom: 14px; }
.grid.four { grid-template-columns: repeat(4, minmax(0, 1fr)); }
.grid.quota { grid-template-columns: 1fr 1fr 1.6fr; }
.card {
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}
.stat { padding: 16px 18px 15px; min-width: 0; }
.k { color: var(--muted); font-size: 12px; letter-spacing: .06em; }
.v { margin-top: 6px; font: 600 24px/1.2 var(--serif); color: var(--ink); font-variant-numeric: tabular-nums; }
.v .unit { margin-left: 6px; font: 400 13px/1 var(--sans); color: var(--muted); }
.v.sm { font: 600 14.5px/1.45 var(--sans); color: var(--text); }
.s { margin-top: 6px; color: var(--muted); font-size: 12.5px; line-height: 1.55; }
.bar { position: relative; height: 8px; margin-top: 12px; border-radius: 999px; background: var(--bg-deep); overflow: visible; }
.bar .fill { position: absolute; inset: 0 auto 0 0; border-radius: inherit; background: var(--ok); transition: width .6s ease; }
.bar.tone-warn .fill { background: var(--warn); }
.bar.tone-err .fill { background: var(--err); }
.bar.tone-idle .fill { background: var(--idle); }
.bar .stopline { position: absolute; top: -4px; bottom: -4px; width: 2px; margin-left: -1px; border-radius: 2px; background: var(--ink); opacity: .55; }

/* controls */
.controls {
  display: flex; gap: 12px 18px; align-items: center; flex-wrap: wrap;
  padding: 12px 16px; margin: 0 0 30px;
  background: var(--card-soft);
}
.ctl { display: inline-flex; align-items: center; gap: 10px; }
.field { display: inline-flex; align-items: center; gap: 4px; padding: 0 10px 0 2px; background: var(--card); border: 1px solid var(--line-strong); border-radius: 10px; transition: border-color .15s, box-shadow .15s; }
.field:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.field input { width: 3.4rem; border: 0; outline: 0; background: transparent; color: var(--ink); padding: 7px 0 7px 8px; font: 600 15px var(--sans); font-variant-numeric: tabular-nums; }
.field span { color: var(--muted); }
.controls .hint { flex: 1 1 18rem; margin: 0; color: var(--muted); font-size: 12.5px; }
.btn {
  display: inline-flex; align-items: center; justify-content: center; gap: 6px;
  padding: 7px 13px; border-radius: 10px; cursor: pointer;
  font: 600 12.5px/1.2 var(--sans); text-decoration: none; white-space: nowrap;
  color: var(--ink); background: var(--card); border: 1px solid var(--line-strong);
  transition: background .15s, border-color .15s, color .15s, transform .15s;
}
.btn:hover { border-color: var(--muted); }
.btn:active { transform: translateY(1px); }
.btn.primary { color: #fffaf4; background: var(--accent); border-color: var(--accent); }
.btn.primary:hover { background: #a65b34; border-color: #a65b34; }
.btn.quiet { background: transparent; border-color: transparent; color: var(--muted); font-weight: 500; }
.btn.quiet.danger:hover { color: var(--err); background: var(--err-soft); }
.btn[aria-busy="true"] { opacity: .6; pointer-events: none; }
.toggle {
  display: inline-flex; align-items: center; gap: 9px; text-decoration: none;
  font-size: 12.5px; font-weight: 600; color: var(--text-2);
}
.switch {
  position: relative; display: inline-block; flex: none;
  width: 38px; height: 22px; border-radius: 999px;
  background: var(--line-strong); transition: background .2s;
  text-decoration: none;
}
.switch::after {
  content: ""; position: absolute; top: 3px; left: 3px;
  width: 16px; height: 16px; border-radius: 50%;
  background: #fff; box-shadow: 0 1px 2px rgba(60, 45, 25, .3);
  transition: transform .2s;
}
.switch.on { background: var(--ok); }
.switch.on::after { transform: translateX(16px); }
.toggle:hover .switch, .switch:hover { filter: brightness(.96); }

/* sessions */
.section-head { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; margin: 0 0 14px; }
h2 { margin: 0; font: 600 19px/1.3 var(--serif); color: var(--ink); }
.count { display: inline-grid; place-items: center; min-width: 24px; height: 22px; padding: 0 7px; border-radius: 999px; background: var(--bg-deep); color: var(--text-2); font-size: 12px; font-weight: 600; }
.section-head .note { color: var(--muted); font-size: 12.5px; }
.list { display: flex; flex-direction: column; gap: 12px; }
.session {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  grid-template-areas: "sw body act";
  gap: 6px 16px; align-items: start;
  padding: 16px 18px 14px;
  position: relative; overflow: hidden;
  transition: box-shadow .2s, border-color .2s;
}
.session:hover { box-shadow: var(--shadow-lift); }
.session::before { content: ""; position: absolute; inset: 0 auto 0 0; width: 3px; background: transparent; }
.session.on::before { background: var(--ok); }
.session.error::before { background: var(--err); }
.session.off { background: var(--card-soft); }
.session > .switch { grid-area: sw; margin-top: 7px; }
.session > .body { grid-area: body; min-width: 0; }
.session > .actions { grid-area: act; display: flex; flex-direction: column; gap: 6px; align-items: stretch; }
.rename { display: flex; align-items: center; gap: 6px; margin: 0 0 6px; }
.rename .name {
  flex: 1; min-width: 0; margin-left: -8px;
  padding: 4px 8px; border-radius: 8px;
  border: 1px solid transparent; background: transparent; outline: 0;
  color: var(--ink); font: 600 17px/1.35 var(--serif);
  text-overflow: ellipsis;
  transition: background .15s, border-color .15s;
}
.rename .name:hover { background: var(--card-soft); border-color: var(--line); }
.rename .name:focus { background: var(--card); border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.rename .btn { opacity: 0; pointer-events: none; transition: opacity .15s; }
.rename:focus-within .btn, .rename:hover .btn { opacity: 1; pointer-events: auto; }
.tags { display: flex; flex-wrap: wrap; gap: 6px; }
.tag {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 2px 9px; border-radius: 999px;
  font-size: 12px; line-height: 1.6; white-space: nowrap;
  color: var(--text-2); background: var(--bg); border: 1px solid var(--line);
  font-variant-numeric: tabular-nums;
}
.tag.src-ai { color: var(--accent); background: var(--accent-soft); border-color: color-mix(in srgb, var(--accent) 22%, transparent); }
.tag.src-custom { color: var(--armed); background: var(--armed-soft); border-color: color-mix(in srgb, var(--armed) 20%, transparent); }
.tag.src-prompt { color: var(--muted); border-style: dashed; }
.tag.bg { color: #6d5a99; background: #eeeaf6; border-color: #ddd5ee; }
.tag.bg::before { content: ""; width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.times { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px 18px; margin: 12px 0 0; padding-top: 11px; border-top: 1px dashed var(--line); }
.times div { min-width: 0; }
.times dt { color: var(--muted); font-size: 11.5px; letter-spacing: .05em; }
.times dd { margin: 2px 0 0; font-size: 12.5px; color: var(--text-2); font-variant-numeric: tabular-nums; }
.times em { font-style: normal; color: var(--muted); }
.times .cache { color: var(--ok); }
.row-err { margin-top: 10px; padding: 7px 11px; border-radius: 9px; color: var(--err); background: var(--err-soft); font-size: 12.5px; }
.empty { padding: 26px 22px; text-align: center; color: var(--text-2); border: 1.5px dashed var(--line-strong); border-radius: var(--radius); background: var(--card-soft); }
.empty .big { display: block; margin-bottom: 6px; font: 600 17px var(--serif); color: var(--ink); }
.hints { margin: 16px 2px 0; padding: 0; list-style: none; color: var(--muted); font-size: 12.5px; display: grid; gap: 4px; }
.hints li { position: relative; padding-left: 14px; }
.hints li::before { content: ""; position: absolute; left: 2px; top: .7em; width: 4px; height: 4px; border-radius: 50%; background: var(--line-strong); }
.alert { margin: 0 0 14px; padding: 12px 16px; border-radius: 12px; font-size: 13.5px; border: 1px solid transparent; }
.alert.warn { color: #87601a; background: var(--warn-soft); border-color: #eedcb5; }
.alert.err { color: var(--err); background: var(--err-soft); border-color: #efcdc5; white-space: pre-wrap; margin-top: 16px; }

/* quota log */
.log { margin-top: 30px; padding: 18px 18px 10px; }
.log .sub { margin: 4px 0 14px; color: var(--muted); font-size: 12.5px; }
.scroll { overflow-x: auto; margin: 0 -6px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; font-variant-numeric: tabular-nums; }
th { text-align: left; color: var(--muted); font-weight: 500; font-size: 11.5px; letter-spacing: .05em; padding: 7px 10px; border-bottom: 1px solid var(--line); white-space: nowrap; }
td { padding: 10px; border-bottom: 1px solid color-mix(in srgb, var(--line) 70%, transparent); vertical-align: top; color: var(--text-2); }
tbody tr:last-child td { border-bottom: 0; }
tbody tr:hover td { background: var(--card-soft); }
td .raw, td.raw { color: var(--muted); font: 11.5px/1.5 var(--mono); word-break: break-all; }
td.raw { min-width: 16rem; }
.kind { display: inline-block; padding: 1px 8px; border-radius: 999px; font-size: 12px; background: var(--bg); border: 1px solid var(--line); }
tr.ok td.util { color: var(--ok); font-weight: 650; }
tr.miss td.util { color: var(--warn); }
tr.fail td.util { color: var(--err); font-weight: 650; }
code { font: 12px var(--mono); background: var(--bg-deep); color: var(--ink); padding: .1rem .4rem; border-radius: 6px; }
.foot { margin-top: 26px; display: flex; justify-content: center; gap: 8px; flex-wrap: wrap; color: var(--muted); font-size: 12px; }
.foot span + span::before { content: "·"; margin-right: 8px; color: var(--line-strong); }

@media (max-width: 860px) {
  .grid.four { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .grid.quota { grid-template-columns: 1fr 1fr; }
  .grid.quota > :last-child { grid-column: 1 / -1; }
  .times { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 600px) {
  main { padding: 26px 16px 40px; }
  h1 { font-size: 25px; }
  .grid.four, .grid.quota { grid-template-columns: 1fr 1fr; gap: 10px; }
  .stat { padding: 13px 14px; }
  .session { grid-template-columns: auto minmax(0, 1fr); grid-template-areas: "sw body" "act act"; padding: 14px; }
  .session > .actions { flex-direction: row; justify-content: flex-end; }
  .times { grid-template-columns: 1fr; }
  .rename .btn { opacity: 1; pointer-events: auto; }
}
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
</head>
<body>
<main id="view">
  <header class="top">
    <div>
      <div class="eyebrow"><span class="mark" aria-hidden="true"></span>CLIProxyAPI 插件 · v{{.Version}}</div>
      <h1>Claude 缓存保活</h1>
      <p class="lead">{{.StateHint}}</p>
    </div>
    <div class="badge state-{{.State}}"><span class="dot"></span>{{.StateLabel}}</div>
  </header>

  <section class="grid four">
    <div class="card stat"><div class="k">会话</div><div class="v">{{.SessionCount}}<span class="unit">/ {{.MaxSessions}}</span></div><div class="s">{{.EnabledCount}} 路保活中{{if .BackgroundCnt}} · {{.BackgroundCnt}} 路后台会话{{end}}</div></div>
    <div class="card stat"><div class="k">间隔</div><div class="v">{{.IntervalMin}}<span class="unit">分钟</span></div><div class="s">输出上限 max_tokens={{.MaxTokens}}</div></div>
    <div class="card stat"><div class="k">上次保活</div><div class="v sm">{{.LastPing}}</div><div class="s">{{.LastPingAgo}}</div></div>
    <div class="card stat"><div class="k">最近一次到期</div><div class="v sm">{{.NextPing}}</div><div class="s">{{.NextPingAgo}}</div></div>
  </section>

  <section class="grid quota">
    <div class="card stat">
      <div class="k">5 小时已用</div>
      <div class="v">{{.BudgetUsed}}</div>
      <div class="bar tone-{{.Tone5h}}"><span class="fill" style="width: {{.Bar5h}}%"></span>{{if .GuardChat}}<span class="stopline" style="left: {{.StopPercent}}%" title="对话停线 {{.StopPercent}}%"></span>{{end}}</div>
      <div class="s">CPA 对话停在 {{.BudgetBlock}}{{if .HasReset}} · 刷新 {{.ResetAt}}（{{.ResetIn}}）{{else if .UsedKnown}} · 还没读到刷新时间{{end}}</div>
    </div>
    <div class="card stat">
      <div class="k">7 天已用</div>
      <div class="v">{{.BudgetUsed7d}}</div>
      <div class="bar tone-{{.Tone7d}}"><span class="fill" style="width: {{.Bar7d}}%"></span></div>
      <div class="s">{{if .HasReset7d}}刷新 {{.Reset7dAt}}（{{.Reset7dIn}}）{{else if .Used7dKnown}}还没读到刷新时间{{else}}等下一发带上 7 天用量头{{end}}</div>
    </div>
    <div class="card stat">
      <div class="k">CPA 对话 / 保活</div>
      <div class="v">{{.BudgetChat}}<span class="unit">/ {{.BudgetKeep}}</span></div>
      <div class="s">{{.BudgetNote}}</div>
    </div>
  </section>

  <form class="card controls" data-stop action="" method="get">
    <div class="ctl">
      <label class="k" for="stop-input">对话停在</label>
      <span class="field"><input id="stop-input" name="stop" type="number" min="60" max="99" step="1" value="{{.StopPercent}}" aria-label="对话停在百分之几"><span>%</span></span>
      <button class="btn" type="submit">保存</button>
    </div>
    <a class="toggle" data-action href="{{.GuardHref}}" role="switch" aria-checked="{{if .GuardChat}}true{{else}}false{{end}}"><span class="switch {{if .GuardChat}}on{{end}}"></span>{{if .GuardChat}}正在拦对话{{else}}未拦对话{{end}}</a>
    <p class="hint">默认 95，用量真到这条线才拦，不按下一发估。改完会记住，重启 CPA 也还在。</p>
  </form>

  {{if .ChatBlocked}}
  <div class="alert warn">CPA 对话已停在停线。等到 5 小时额度刷新{{if .HasReset}}（{{.ResetAt}}，{{.ResetIn}}）{{end}}或用量掉回停线。/compact 仍可走。保活不受停线影响。</div>
  {{end}}
  {{if and .KeepPaused (not .ChatBlocked)}}
  <div class="alert warn">{{.BudgetNote}}</div>
  {{end}}

  <section>
    <div class="section-head">
      <h2>会话</h2><span class="count">{{.SessionCount}}</span>
      <span class="note">标题取自 Claude Code 为会话生成的名称，和 /resume 里看到的一致</span>
    </div>
    {{if not .HasSessions}}
    <div class="empty"><span class="big">还没有记下对话</span>把 Claude Code 指到 CPA 的 <code>127.0.0.1:8317</code>，并设置 <code>promptCacheTtl: "1h"</code>。正常完成一轮对话后刷新本页。插件会跳过 <code>count_tokens</code>。</div>
    {{else}}
    <div class="list">
      {{range .Sessions}}
      <article class="card session {{.RowClass}}">
        <a class="switch {{if .Enabled}}on{{end}}" data-action href="{{.ToggleHref}}" role="switch" aria-checked="{{if .Enabled}}true{{else}}false{{end}}" title="{{if .Enabled}}取消保活{{else}}开启保活{{end}}"></a>
        <div class="body">
          <form class="rename" data-rename action="" method="get">
            <input type="hidden" name="rename" value="{{.ID}}">
            <input class="name" name="name" value="{{.Label}}" maxlength="42" aria-label="会话名称" spellcheck="false" title="{{.Label}}">
            <button class="btn" type="submit">改名</button>
          </form>
          <div class="tags">
            <span class="tag src-{{.TitleSource}}">{{.TitleTag}}</span>
            {{if .Background}}<span class="tag bg">后台会话</span>{{end}}
            <span class="tag">{{.Model}}</span>
            <span class="tag">TTL {{.CacheTTL}}</span>
            <span class="tag">{{.Messages}} 条消息</span>
            <span class="tag">{{.BodySize}}</span>
          </div>
          <dl class="times">
            <div><dt>最近请求</dt><dd>{{.SavedAt}} <em>{{.SavedAgo}}</em></dd></div>
            <div><dt>下次保活</dt><dd>{{.NextPing}} <em>{{.NextPingAgo}}</em></dd></div>
            <div><dt>上次保活</dt><dd>{{.LastPing}} <em>{{.LastPingAgo}}</em>{{if ne .PingCache "—"}} <span class="cache">{{.PingCache}}</span>{{end}}</dd></div>
          </dl>
          {{if .PingPaused}}<div class="row-err">连续两次保活未命中，已暂停后续心跳，避免把冷前缀反复按 1h 写入。新对话后会重新开。</div>{{end}}
          {{if .PingError}}<div class="row-err">{{.PingError}}</div>{{end}}
        </div>
        <div class="actions">
          <a class="btn primary" data-action data-ping href="{{.PingHref}}">立刻保活</a>
          <a class="btn quiet danger" data-action href="{{.ForgetHref}}">忘记</a>
        </div>
      </article>
      {{end}}
    </div>
    <ul class="hints">
      <li>新对话默认不保活，打开左侧开关才定时续。满员（比如 8 路）时优先顶掉没开保活的。</li>
      <li>「立刻保活」只打这一路，不等倒计时。</li>
      <li>名称可以自己改；清空再保存会回到 Claude Code 生成的标题，还没拿到标题时用首条消息。</li>
      <li>后台会话（<code>claude --bg</code> 或 agents 视图里的后台会话）照常列出并可保活；Task/Agent 子代理不保活。</li>
      <li>CPA 对话会在上面填的百分比停住，留下一截给保活，也避免最后一发打穿 100%。</li>
      {{if .SubagentNote}}<li>{{.SubagentNote}}</li>{{end}}
    </ul>
    {{end}}
    {{if and .SubagentNote (not .HasSessions)}}
    <ul class="hints"><li>{{.SubagentNote}}</li></ul>
    {{end}}
  </section>

  {{if .PingError}}
  <div class="alert err">上一轮保活错误：{{.PingError}}</div>
  {{end}}

  <section class="card log">
    <h2>额度记录</h2>
    <p class="sub">每次 Claude 响应回来后解析的用量。缓存列看 <code>cache_read</code> / <code>cache_creation</code>；5 小时列是额度头。最新在上，最多 5 条；CPA 重启后清空。</p>
    {{if .HasQuotaLog}}
    <div class="scroll">
      <table>
        <thead>
          <tr><th>时间</th><th>来源</th><th>缓存</th><th>5 小时</th><th>状态</th><th>7 天</th><th>原始头</th></tr>
        </thead>
        <tbody>
          {{range .QuotaLog}}
          <tr class="{{.RowClass}}">
            <td>{{.At}}<div class="raw">{{.Ago}} · {{.Model}}</div></td>
            <td><span class="kind">{{.Kind}}</span></td>
            <td>{{.Cache}}</td>
            <td class="util">{{.Util5h}}</td>
            <td>{{.Status5h}}</td>
            <td>{{.Util7d}}{{if ne .Status7d "—"}}<div class="raw">{{.Status7d}}</div>{{end}}</td>
            <td class="raw">{{.Raw}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
    {{else}}
    <p class="sub">还没有用量回调。用 Claude Code 走 CPA 正常聊一轮后，这里应出现缓存命中情况和 5h-utilization。如果缓存是「—」，说明这次回调没带 token 明细。</p>
    {{end}}
  </section>

  <p class="foot"><span>数据每 15 秒静默更新</span><span>{{.Now}}</span><span>{{.Version}}</span><span>不会显示 prompt 正文</span></p>
</main>
<script>
(function () {
  var timer = null;
  var busy = false;
  function apply(html, url) {
    var doc = new DOMParser().parseFromString(html, "text/html");
    var next = doc.getElementById("view");
    var cur = document.getElementById("view");
    if (!next || !cur) {
      return;
    }
    cur.replaceWith(next);
    if (url && url.indexOf("?") !== -1 && window.history && history.replaceState) {
      history.replaceState(null, "", url.split("?")[0]);
    }
  }
  function load(url) {
    return fetch(url, { cache: "no-store", headers: { Accept: "text/html" }, redirect: "follow" }).then(function (res) {
      if (!res.ok) {
        throw new Error("HTTP " + res.status);
      }
      return res.text().then(function (html) {
        apply(html, res.url || url);
      });
    });
  }
  document.addEventListener("click", function (ev) {
    var a = ev.target.closest("a[data-action]");
    if (!a) {
      return;
    }
    ev.preventDefault();
    if (a.getAttribute("aria-busy") === "true") {
      return;
    }
    if (a.hasAttribute("data-ping")) {
      busy = true;
      a.setAttribute("aria-busy", "true");
      a.textContent = "保活中…";
    }
    load(a.href).catch(function () {
      location.assign(a.href);
    }).then(function () {
      busy = false;
    });
  });
  document.addEventListener("submit", function (ev) {
    var form = ev.target.closest("form[data-rename], form[data-stop]");
    if (!form) {
      return;
    }
    ev.preventDefault();
    var action = form.getAttribute("action") || location.pathname;
    var url = action.split("?")[0] + "?" + new URLSearchParams(new FormData(form)).toString();
    load(url).catch(function () {
      form.submit();
    });
  });
  timer = setInterval(function () {
    if (document.hidden || busy) {
      return;
    }
    if (document.activeElement && document.activeElement.closest("form[data-rename], form[data-stop]")) {
      return;
    }
    load(location.pathname).catch(function () {});
  }, 15000);
  window.addEventListener("pagehide", function () {
    if (timer) {
      clearInterval(timer);
    }
  });
})();
</script>
</body>
</html>`
