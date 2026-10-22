package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Scripting commands: list agent panes, read one, type into one, and wait
// for one to need you. They work the same for any agent or program.

// paneTarget is the SESSION:PANE form bmux agents prints for a pane.
func paneTarget(session string, pane int) string {
	return session + ":" + strconv.Itoa(pane)
}

// target is a parsed TARGET, written like tmux's session:window.pane:
//
//	api       the session (its active window's focused pane)
//	api:7     pane 7, numbered across the session as bmux agents lists it
//	api:2.    window 2 (its focused pane)
//	api:2.1   window 2, the first pane in its layout order
//	:2.1      the same in the session this command runs in
//
// api:N keeps meaning pane N, so targets bmux agents printed keep working;
// the server points tmux users to api:N. when N is a window instead.
type target struct {
	Session             string
	Window, Index, Pane int
}

const targetForms = "SESSION, SESSION:PANE, SESSION:WINDOW. or SESSION:WINDOW.N"

// parseTarget reads a TARGET. An empty session means the calling pane's.
func parseTarget(text string) (target, error) {
	session, rest, found := strings.Cut(text, ":")
	if session == "" && found {
		session = os.Getenv("BMUX_SESSION")
		if session == "" {
			return target{}, fmt.Errorf("target %q: a target starting with : names the session this command runs in, and this isn't a bmux pane", text)
		}
	}
	if err := protocol.ValidateSessionName(session); err != nil {
		return target{}, fmt.Errorf("target %q: %v", text, err)
	}
	t := target{Session: session}
	if !found || rest == "" {
		return t, nil
	}
	number := func(s string) (int, bool) {
		n, err := strconv.Atoi(s)
		return n, err == nil && n > 0 && s[0] != '+'
	}
	window, index, dotted := strings.Cut(rest, ".")
	var ok bool
	switch {
	case !dotted:
		t.Pane, ok = number(rest)
	case index == "":
		t.Window, ok = number(window)
	default:
		t.Window, ok = number(window)
		if ok {
			t.Index, ok = number(index)
		}
	}
	if !ok {
		return target{}, fmt.Errorf("target %q isn't one of %s; numbers start at 1", text, targetForms)
	}
	return t, nil
}

// names reports whether t picks one pane itself, not whatever is focused.
func (t target) names() bool { return t.Pane != 0 || t.Index != 0 }

// request fills in r's target fields.
func (t target) request(r *protocol.Request) {
	r.Name, r.Window, r.PaneIndex, r.PaneID = t.Session, t.Window, t.Index, t.Pane
}

// matches reports whether agent pane p is within t.
func (t target) matches(p agentPane) bool {
	switch {
	case p.Session != t.Session:
		return false
	case t.Pane != 0:
		return p.Pane == t.Pane
	case t.Window != 0 && t.Index != 0:
		return p.Window == t.Window && p.Index == t.Index
	case t.Window != 0:
		return p.Window == t.Window
	}
	return true
}

// agentPane is one agent pane as bmux agents and bmux wait-agent print it.
type agentPane struct {
	Target    string `json:"target"`
	Session   string `json:"session"`
	SessionID uint64 `json:"session_id,omitempty"`
	Pane      int    `json:"pane"`
	Window    int    `json:"window,omitempty"`
	Index     int    `json:"index,omitempty"`
	Agent     string `json:"agent"`
	State     string `json:"state"`
	Seq       uint64 `json:"seq"`
	Attention bool   `json:"attention,omitempty"`
	// Self marks the pane this command runs in.
	Self bool `json:"self,omitempty"`
}

// agentPanes lists every pane with an agent, in list order.
func agentPanes(socket string) ([]agentPane, error) {
	r, err := client.Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		return nil, err
	}
	selfID, _ := strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
	selfPane, _ := strconv.Atoi(os.Getenv("BMUX_PANE"))
	var result []agentPane
	for _, s := range r.Sessions {
		for _, a := range s.Activity {
			if a.Agent == "" {
				continue
			}
			result = append(result, agentPane{
				Target: paneTarget(s.Name, a.Pane), Session: s.Name, SessionID: s.ID, Pane: a.Pane, Window: a.Window, Index: a.Index,
				Agent: a.Agent, State: a.State, Seq: a.Seq, Attention: a.Attention,
				Self: selfID != 0 && s.ID == selfID && a.Pane == selfPane,
			})
		}
	}
	return result, nil
}

func printJSON(v any) error {
	return json.NewEncoder(os.Stdout).Encode(v)
}

func runAgents(socket string, args []string) error {
	asJSON := len(args) == 1 && args[0] == "--json"
	if len(args) > 0 && !asJSON {
		return fmt.Errorf("usage: bmux agents [--json]")
	}
	panes, err := agentPanes(socket)
	if err != nil {
		return err
	}
	if asJSON {
		if panes == nil {
			panes = []agentPane{}
		}
		return printJSON(panes)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TARGET\tAGENT\tSTATE\tSEQ\t")
	for _, p := range panes {
		note := ""
		if p.Self {
			note = "(this pane)"
		} else if p.Attention {
			note = "needs you"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", p.Target, p.Agent, p.State, p.Seq, note)
	}
	return w.Flush()
}

func runCapture(socket string, args []string) error {
	asJSON := false
	var targets []string
	for _, arg := range args {
		if arg == "--json" {
			asJSON = true
		} else {
			targets = append(targets, arg)
		}
	}
	if len(targets) != 1 {
		return fmt.Errorf("usage: bmux capture [--json] TARGET (%s)", targetForms)
	}
	t, err := parseTarget(targets[0])
	if err != nil {
		return err
	}
	req := protocol.Request{Op: "capture-pane"}
	t.request(&req)
	r, err := client.Request(socket, req)
	if err != nil {
		return err
	}
	if !asJSON {
		fmt.Print(r.Text)
		return nil
	}
	return printJSON(struct {
		Target string `json:"target"`
		Window int    `json:"window,omitempty"`
		Index  int    `json:"index,omitempty"`
		Agent  string `json:"agent,omitempty"`
		State  string `json:"state,omitempty"`
		Seq    uint64 `json:"seq"`
		Text   string `json:"text"`
	}{paneTarget(t.Session, r.Pane.Pane), r.Pane.Window, r.Pane.Index, r.Pane.Agent, r.Pane.State, r.Pane.Seq, r.Text})
}

// sendRequest parses bmux send's arguments: one target and, in order, the
// keys and text to type. Flags may come before or after the target.
func sendRequest(args []string) (protocol.Request, error) {
	const usage = "usage: bmux send TARGET [--if-seq N] (--key KEY | --text TEXT)..., where TARGET names a pane: SESSION:PANE or SESSION:WINDOW.N"
	r := protocol.Request{Op: "send-input"}
	target := ""
	for i := 0; i < len(args); i++ {
		arg, value, inline := strings.Cut(args[i], "=")
		takesValue := arg == "--key" || arg == "--text" || arg == "--if-seq"
		if takesValue && !inline {
			if i+1 == len(args) {
				return r, fmt.Errorf("%s needs a value; %s", arg, usage)
			}
			i++
			value = args[i]
		}
		switch {
		case arg == "--key":
			key, err := parseKey(value)
			if err != nil {
				return r, err
			}
			r.Input = append(r.Input, key)
		case arg == "--text":
			if value == "" {
				return r, fmt.Errorf("--text needs some text")
			}
			r.Input = append(r.Input, protocol.Input{Paste: value})
		case arg == "--if-seq":
			seq, err := strconv.ParseUint(value, 10, 64)
			if err != nil || seq == 0 {
				return r, fmt.Errorf("--if-seq needs the seq bmux agents or bmux capture --json printed")
			}
			r.IfSeq = seq
		case strings.HasPrefix(args[i], "-"):
			return r, fmt.Errorf("unknown flag %s; %s", args[i], usage)
		case target != "":
			return r, fmt.Errorf("one target only; %s", usage)
		default:
			target = args[i]
		}
	}
	if target == "" || len(r.Input) == 0 {
		return r, errors.New(usage)
	}
	t, err := parseTarget(target)
	if err != nil {
		return r, err
	}
	if !t.names() {
		return r, fmt.Errorf("send needs a pane (SESSION:PANE or SESSION:WINDOW.N), so input never reaches a pane by accident; bmux agents lists targets")
	}
	t.request(&r)
	return r, nil
}

func runSend(socket string, args []string) error {
	r, err := sendRequest(args)
	if err != nil {
		return err
	}
	_, err = client.Request(socket, r)
	return err
}

// parseKey turns a key name, written as in the config file (enter, down,
// esc, C-c, A-x, F5, a), into a key press.
func parseKey(name string) (protocol.Input, error) {
	canonical, err := config.KeyName(name)
	if err != nil {
		return protocol.Input{}, err
	}
	var in protocol.Input
	for len(canonical) > 2 && canonical[1] == '-' {
		switch canonical[0] {
		case 'C':
			in.Mod |= int(uv.ModCtrl)
		case 'A':
			in.Mod |= int(uv.ModAlt)
		case 'S':
			in.Mod |= int(uv.ModShift)
		}
		canonical = canonical[2:]
	}
	named := map[string]rune{
		"left": uv.KeyLeft, "right": uv.KeyRight, "up": uv.KeyUp, "down": uv.KeyDown,
		"home": uv.KeyHome, "end": uv.KeyEnd, "pageup": uv.KeyPgUp, "pagedown": uv.KeyPgDown,
		"ins": uv.KeyInsert, "del": uv.KeyDelete, "backspace": uv.KeyBackspace, "tab": uv.KeyTab,
		"ret": uv.KeyEnter, "esc": uv.KeyEscape, "space": uv.KeySpace, "minus": '-',
	}
	if code, ok := named[canonical]; ok {
		in.Code = code
	} else if n, err := strconv.Atoi(strings.TrimPrefix(canonical, "F")); err == nil && strings.HasPrefix(canonical, "F") {
		in.Code = uv.KeyF1 + rune(n-1)
	} else {
		in.Code, _ = utf8.DecodeRuneInString(canonical)
	}
	return in, nil
}

// waitFor is what bmux wait-agent waits for.
type waitFor struct {
	states  []string
	since   uint64
	targets []target // none means every pane
}

// match picks the earliest change after since among panes in a wanted
// state, skipping the pane the command runs in, so a supervisor never wakes
// itself. Reporting the lowest seq first lets repeated waits with
// --since walk through every change in order.
func (w waitFor) match(panes []agentPane) (agentPane, bool) {
	var best agentPane
	found := false
	for _, p := range panes {
		if p.Self || p.Seq <= w.since || !slices.Contains(w.states, p.State) || !w.wants(p) {
			continue
		}
		if !found || p.Seq < best.Seq {
			best, found = p, true
		}
	}
	return best, found
}

func (w waitFor) wants(p agentPane) bool {
	if len(w.targets) == 0 {
		return true
	}
	return slices.ContainsFunc(w.targets, func(t target) bool { return t.matches(p) })
}

const waitPollInterval = time.Second

func runWaitAgent(socket string, args []string) error {
	const usage = "usage: bmux wait-agent [--state S,...] [--since SEQ] [--timeout DURATION | --notify SESSION:PANE] [TARGET...]"
	w := waitFor{states: []string{protocol.AgentBlocked, protocol.AgentIdle}}
	var timeout time.Duration
	notify, sinceGiven := "", false
	for i := 0; i < len(args); i++ {
		arg, value, inline := strings.Cut(args[i], "=")
		if arg == "--state" || arg == "--since" || arg == "--timeout" || arg == "--notify" {
			if !inline {
				if i+1 == len(args) {
					return fmt.Errorf("%s needs a value; %s", arg, usage)
				}
				i++
				value = args[i]
			}
		}
		switch {
		case arg == "--state":
			w.states = strings.Split(value, ",")
			for _, s := range w.states {
				if s != protocol.AgentWorking && s != protocol.AgentBlocked && s != protocol.AgentIdle {
					return fmt.Errorf("--state takes working, blocked and idle, comma-separated")
				}
			}
		case arg == "--since":
			seq, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return fmt.Errorf("--since needs a seq number")
			}
			w.since, sinceGiven = seq, true
		case arg == "--timeout":
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 {
				return fmt.Errorf("--timeout needs a duration such as 30s or 10m")
			}
			timeout = d
		case arg == "--notify":
			t, err := parseTarget(value)
			if err != nil || t.Pane == 0 {
				return fmt.Errorf("--notify needs the SESSION:PANE of the agent to tell; bmux agents lists them")
			}
			notify = paneTarget(t.Session, t.Pane) // as bmux agents prints it, so :3 works too
		case strings.HasPrefix(args[i], "-"):
			return fmt.Errorf("unknown flag %s; %s", args[i], usage)
		default:
			t, err := parseTarget(args[i])
			if err != nil {
				return err
			}
			w.targets = append(w.targets, t)
		}
	}
	if notify != "" {
		if timeout > 0 {
			return fmt.Errorf("--notify runs until stopped; it takes no --timeout")
		}
		return runNotify(socket, &notifier{w: w, target: notify, sinceGiven: sinceGiven, pending: map[string]agentPane{}})
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		panes, err := agentPanes(socket)
		if err != nil {
			return err
		}
		if p, ok := w.match(panes); ok {
			return printJSON(p)
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s; nothing reached %s", timeout, strings.Join(w.states, " or "))
		}
		time.Sleep(waitPollInterval)
	}
}

// notifier is bmux wait-agent --notify: instead of exiting at the first
// change, it keeps each pane's newest wanted change and types them one at a
// time into a supervising agent's pane, only while that agent is idle. The
// supervisor never runs a waiting command itself, so nothing holds its turn.
type notifier struct {
	w          waitFor
	target     string // the supervisor's SESSION:PANE
	sinceGiven bool
	started    bool
	pending    map[string]agentPane // by target
	// typedAt is the supervisor's seq when it was last typed into; nothing
	// more is typed until its state has moved on from that.
	typedAt uint64
}

// observe folds one listing into pending. A pane's newer change replaces
// its older one, a pane that moved to an unwanted state or went away drops
// out, and the supervisor's own changes never count. Without --since, the
// first listing counts only panes already blocked: they still need the
// user, while earlier idle turns are old news.
func (n *notifier) observe(panes []agentPane) {
	seen := make(map[string]bool, len(panes))
	since := n.w.since
	for _, p := range panes {
		seen[p.Target] = true
		n.w.since = max(n.w.since, p.Seq)
		if p.Self || p.Target == n.target || p.Seq <= since || !n.w.wants(p) {
			continue
		}
		wanted := slices.Contains(n.w.states, p.State)
		if !n.started && !n.sinceGiven {
			wanted = wanted && p.State == protocol.AgentBlocked
		}
		if wanted {
			n.pending[p.Target] = p
		} else {
			delete(n.pending, p.Target)
		}
	}
	for target := range n.pending {
		if !seen[target] {
			delete(n.pending, target)
		}
	}
	n.started = true
}

// next is the pending change to type now, earliest first, and the
// supervisor's seq to guard the typing with. ok is false while the
// supervisor is busy, blocked, or hasn't reacted to the last message yet.
func (n *notifier) next(panes []agentPane) (p agentPane, guard uint64, ok bool) {
	i := slices.IndexFunc(panes, func(p agentPane) bool { return p.Target == n.target })
	if i < 0 || panes[i].State != protocol.AgentIdle || panes[i].Seq == n.typedAt || len(n.pending) == 0 {
		return agentPane{}, 0, false
	}
	first := true
	for _, candidate := range n.pending {
		if first || candidate.Seq < p.Seq {
			p, first = candidate, false
		}
	}
	return p, panes[i].Seq, true
}

// notifyMessage is the one line typed into the supervisor: what happened
// and the commands that read and answer it.
func notifyMessage(p agentPane) string {
	what := "finished its turn and is idle"
	if p.State == protocol.AgentBlocked {
		what = "is blocked and waiting on the user"
	}
	return fmt.Sprintf("[bmux] %s (%s) %s, seq %d. Read it with bmux capture --json %s; answer with bmux send %s --if-seq %d.",
		p.Target, p.Agent, what, p.Seq, p.Target, p.Target, p.Seq)
}

func runNotify(socket string, n *notifier) error {
	t, _ := parseTarget(n.target)
	fmt.Printf("Telling %s about agents that are blocked or idle; Ctrl-c stops.\n", n.target)
	for {
		panes, err := agentPanes(socket)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(panes, func(p agentPane) bool { return p.Target == n.target }) {
			return fmt.Errorf("%s has no agent; --notify types only into an agent's pane, while it's idle", n.target)
		}
		n.observe(panes)
		if p, guard, ok := n.next(panes); ok {
			_, err := client.Request(socket, protocol.Request{Op: "send-input", Name: t.Session, PaneID: t.Pane, IfSeq: guard,
				Input: []protocol.Input{{Paste: notifyMessage(p)}, {Code: uv.KeyEnter}}})
			switch {
			case err == nil:
				delete(n.pending, p.Target)
				n.typedAt = guard
				fmt.Printf("%s told about %s (%s, seq %d)\n", time.Now().Format("15:04:05"), p.Target, p.State, p.Seq)
			case !strings.HasPrefix(err.Error(), "stale"):
				return err
			}
			// Stale: the supervisor got busy just now; try once it's idle.
		}
		time.Sleep(waitPollInterval)
	}
}
