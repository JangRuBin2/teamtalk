package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const (
	coopMaxPlayers = 4
	coopMaxText    = 500
)

// Msg is one line of the co-op protocol (JSON per line over TCP).
// N is always 1-based (suspect or evidence number).
type Msg struct {
	T       string    `json:"t"`
	Name    string    `json:"name,omitempty"`
	Host    bool      `json:"host,omitempty"`
	N       int       `json:"n,omitempty"`
	Text    string    `json:"text,omitempty"`
	Title   string    `json:"title,omitempty"`
	From    string    `json:"from,omitempty"`
	Time    string    `json:"time,omitempty"`
	Players []string  `json:"players,omitempty"`
	Game    *CoopInfo `json:"game,omitempty"`
	End     *CoopEnd  `json:"end,omitempty"`
}

type CoopInfo struct {
	Company  string        `json:"company"`
	Boss     string        `json:"boss"`
	Incident string        `json:"incident"`
	Briefing string        `json:"briefing"`
	Suspects []SuspectInfo `json:"suspects"`
	Evidence []EviInfo     `json:"evidence"`
	Started  time.Time     `json:"started"`
	Deadline time.Time     `json:"deadline"`
	Practice bool          `json:"practice"`
}

type SuspectInfo struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Dept  string `json:"dept"`
}

type EviInfo struct {
	Title string `json:"title"`
	Owner string `json:"owner"`
	Mine  bool   `json:"mine"`
}

type CoopEnd struct {
	Win      bool     `json:"win"`
	Timeout  bool     `json:"timeout"`
	Accused  string   `json:"accused"`
	Grade    string   `json:"grade"`
	Feedback string   `json:"feedback"`
	Culprit  string   `json:"culprit"`
	Motive   string   `json:"motive"`
	Method   string   `json:"method"`
	Key      string   `json:"key"`
	Timeline []string `json:"timeline"`
	Secrets  []string `json:"secrets"`
}

type coopPlayer struct {
	name   string
	wmu    sync.Mutex
	conn   net.Conn // nil while disconnected
	online bool
	chats  [][]Turn // per suspect; each player interrogates separately
	busy   []bool   // waiting for that suspect's reply
	vote   int      // 1-based suspect, 0 = none
	reason string
}

func (p *coopPlayer) send(m Msg) {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if p.conn == nil {
		return
	}
	b, _ := json.Marshal(m)
	p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	p.conn.Write(append(b, '\n'))
}

func (p *coopPlayer) setConn(c net.Conn) {
	p.wmu.Lock()
	p.conn = c
	p.wmu.Unlock()
}

// outbox collects messages while s.mu is held so they are written after unlocking.
type outbox []struct {
	p *coopPlayer
	m Msg
}

func (o *outbox) to(p *coopPlayer, m Msg) {
	*o = append(*o, struct {
		p *coopPlayer
		m Msg
	}{p, m})
}

func (o outbox) flush() {
	for _, e := range o {
		e.p.send(e.m)
	}
}

// coopServer runs the shared case on the host's machine. Every player,
// the host included, talks to it through serve() over a net.Conn.
type coopServer struct {
	mu       sync.Mutex
	host     *coopPlayer
	players  []*coopPlayer
	state    string // lobby, generating, playing, over
	scen     *Scenario
	owners   []*coopPlayer // evidence -> owner
	started  time.Time
	deadline time.Time
	practice bool
	fired    []bool
	done     chan struct{}
}

func newCoopServer() *coopServer {
	s := &coopServer{state: "lobby", done: make(chan struct{})}
	go s.tickLoop()
	return s
}

func (s *coopServer) close() {
	close(s.done)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.players {
		p.wmu.Lock()
		if p.conn != nil {
			p.conn.Close()
		}
		p.wmu.Unlock()
	}
}

func (s *coopServer) tickLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.tick()
		}
	}
}

// all queues m for every connected player. Call with s.mu held.
func (s *coopServer) all(o *outbox, m Msg) {
	for _, p := range s.players {
		if p.online {
			o.to(p, m)
		}
	}
}

func (s *coopServer) online() []*coopPlayer {
	var on []*coopPlayer
	for _, p := range s.players {
		if p.online {
			on = append(on, p)
		}
	}
	return on
}

func (s *coopServer) names() []string {
	var n []string
	for _, p := range s.online() {
		n = append(n, p.name)
	}
	return n
}

func sysMsg(format string, a ...any) Msg { return Msg{T: "system", Text: fmt.Sprintf(format, a...)} }
func errMsg(format string, a ...any) Msg { return Msg{T: "error", Text: fmt.Sprintf(format, a...)} }

func (s *coopServer) serve(c net.Conn) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if !sc.Scan() {
		return
	}
	c.SetReadDeadline(time.Time{})
	var hello Msg
	if json.Unmarshal(sc.Bytes(), &hello) != nil || hello.T != "hello" {
		return
	}
	p, reject := s.join(c, hello)
	if p == nil {
		b, _ := json.Marshal(errMsg("%s", reject))
		c.Write(append(b, '\n'))
		return
	}
	defer s.leave(p)
	for sc.Scan() {
		var m Msg
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			s.dispatch(p, m)
		}
	}
}

func (s *coopServer) join(c net.Conn, hello Msg) (*coopPlayer, string) {
	var o outbox
	s.mu.Lock()
	defer func() { s.mu.Unlock(); o.flush() }()

	name := strings.TrimSpace(hello.Name)
	if name == "" || utf8.RuneCountInString(name) > 10 {
		return nil, "닉네임은 1~10자로 해주세요."
	}
	for _, p := range s.players {
		if p.name != name {
			continue
		}
		if p.online {
			return nil, "같은 닉네임이 이미 접속해 있습니다."
		}
		p.setConn(c)
		p.online = true
		o.to(p, Msg{T: "welcome", Name: name, Host: p == s.host})
		s.all(&o, sysMsg("%s님이 다시 접속했습니다.", name))
		s.all(&o, Msg{T: "lobby", Players: s.names()})
		if s.scen != nil {
			o.to(p, Msg{T: "game", Game: s.info(p)})
		}
		return p, ""
	}
	if s.state != "lobby" {
		return nil, "이미 게임이 시작됐습니다. (접속이 끊긴 사람은 같은 닉네임으로 다시 들어올 수 있어요)"
	}
	if len(s.online()) >= coopMaxPlayers {
		return nil, fmt.Sprintf("방이 꽉 찼습니다. (최대 %d명)", coopMaxPlayers)
	}
	p := &coopPlayer{name: name, online: true}
	p.setConn(c)
	if s.host == nil {
		s.host = p
	}
	s.players = append(s.players, p)
	o.to(p, Msg{T: "welcome", Name: name, Host: p == s.host})
	s.all(&o, sysMsg("%s님이 들어왔습니다. (%d/%d)", name, len(s.online()), coopMaxPlayers))
	s.all(&o, Msg{T: "lobby", Players: s.names()})
	return p, ""
}

func (s *coopServer) leave(p *coopPlayer) {
	var o outbox
	s.mu.Lock()
	defer func() { s.mu.Unlock(); o.flush() }()
	p.online = false
	p.setConn(nil)
	p.vote, p.reason = 0, ""
	s.all(&o, sysMsg("%s님의 연결이 끊겼습니다. 같은 닉네임으로 다시 들어올 수 있어요.", p.name))
	s.all(&o, Msg{T: "lobby", Players: s.names()})
	if s.state != "playing" {
		return
	}
	// Hand the absent player's evidence to someone still here.
	on := s.online()
	if len(on) == 0 {
		return
	}
	moved := false
	for i, owner := range s.owners {
		if owner == p {
			s.owners[i] = on[i%len(on)]
			moved = true
		}
	}
	if moved {
		s.all(&o, sysMsg("%s님이 가지고 있던 자료 권한을 남은 팀원에게 넘겼습니다. (/e 로 확인)", p.name))
		for _, q := range on {
			o.to(q, Msg{T: "game", Game: s.info(q)})
		}
	}
}

// info builds the per-player view of the case. Call with s.mu held.
func (s *coopServer) info(p *coopPlayer) *CoopInfo {
	sc := s.scen
	in := &CoopInfo{Company: sc.Company, Boss: sc.BossName, Incident: sc.IncidentTitle, Briefing: sc.Briefing,
		Started: s.started, Deadline: s.deadline, Practice: s.practice}
	for _, x := range sc.Suspects {
		in.Suspects = append(in.Suspects, SuspectInfo{x.Name, x.Title, x.Dept})
	}
	for i, e := range sc.Evidence {
		in.Evidence = append(in.Evidence, EviInfo{Title: e.Title, Owner: s.owners[i].name, Mine: s.owners[i] == p})
	}
	return in
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > coopMaxText {
		s = string(r[:coopMaxText])
	}
	return s
}

func (s *coopServer) dispatch(p *coopPlayer, m Msg) {
	switch m.T {
	case "start":
		s.start(p, m.Text == "ai")
	case "team":
		if text := clip(m.Text); text != "" {
			var o outbox
			s.mu.Lock()
			s.all(&o, Msg{T: "team", From: p.name, Text: text, Time: fmtClock(now())})
			s.mu.Unlock()
			o.flush()
		}
	case "dm":
		s.dm(p, m.N, clip(m.Text))
	case "evi", "share":
		s.evidence(p, m.N, m.T == "share")
	case "vote":
		s.vote(p, m.N, clip(m.Text))
	}
}

func (s *coopServer) start(p *coopPlayer, fresh bool) {
	var o outbox
	s.mu.Lock()
	defer func() { s.mu.Unlock(); o.flush() }()
	if p != s.host {
		o.to(p, errMsg("게임 시작은 방장만 할 수 있습니다."))
		return
	}
	if s.state != "lobby" {
		return
	}
	s.state = "generating"
	if fresh {
		s.all(&o, sysMsg("AI가 새 사건을 만드는 중입니다... (1~3분)"))
	} else {
		s.all(&o, sysMsg("사건 파일을 받는 중입니다... (30초 안팎)"))
	}
	go s.generate(fresh)
}

func (s *coopServer) generate(fresh bool) {
	start := now()
	_, off := workDay(start)
	practice := off.Sub(start) < 30*time.Minute
	deadline := fmt.Sprintf("오늘 %s 퇴근 전", cfg.WorkEnd)
	if practice {
		deadline = "오늘 안"
	}
	var sc *Scenario
	var err error
	if fresh {
		sc, err = GenerateScenario(ctx, llm, fmtClock(start), deadline)
	} else {
		sc, err = BuildFromDraft(ctx, llm, deadline)
	}

	var o outbox
	s.mu.Lock()
	defer func() { s.mu.Unlock(); o.flush() }()
	if err != nil {
		s.state = "lobby"
		s.all(&o, errMsg("사건 생성 실패: %s 방장이 /start 로 다시 시도해 주세요.", friendlyError(err)))
		return
	}
	s.players = s.online() // drop whoever left the lobby
	s.scen, s.started, s.deadline, s.practice = sc, start, off, practice
	s.fired = make([]bool, len(pressureTexts))
	s.owners = make([]*coopPlayer, len(sc.Evidence))
	for i := range s.owners {
		s.owners[i] = s.players[i%len(s.players)]
	}
	for _, q := range s.players {
		q.chats = make([][]Turn, len(sc.Suspects))
		q.busy = make([]bool, len(sc.Suspects))
	}
	s.state = "playing"
	for _, q := range s.players {
		o.to(q, Msg{T: "game", Game: s.info(q)})
	}
}

func coopNPCPrompt(sc *Scenario, idx int, player string, team []string) string {
	return npcSystemPrompt(sc, idx) + fmt.Sprintf(`

[조사팀] 지금 %s, 이렇게 여러 명이 함께 조사 중이고 각자 따로 너에게 DM을 보낸다. 이 대화의 상대는 %s다. 다른 팀원과 나눈 대화 내용은 여기서는 알 수 없지만, 다른 팀원도 너에게 연락했을 수 있다는 건 안다.`, strings.Join(team, ", "), player)
}

func (s *coopServer) dm(p *coopPlayer, n int, text string) {
	s.mu.Lock()
	if s.state != "playing" || text == "" || n < 1 || n > len(s.scen.Suspects) {
		s.mu.Unlock()
		return
	}
	i := n - 1
	if p.busy[i] {
		s.mu.Unlock()
		p.send(errMsg("%s님 답장을 기다리는 중입니다.", s.scen.Suspects[i].Name))
		return
	}
	p.busy[i] = true
	hist := append([]Turn(nil), p.chats[i]...)
	system := coopNPCPrompt(s.scen, i, p.name, playerNames(s.players))
	s.mu.Unlock()

	go func() {
		t, err := chatTurn(ctx, llm.npcModel, system, hist, Turn{Time: fmtClock(now()), User: text}, anthropic.BetaOutputConfigEffortLow, nil)
		s.mu.Lock()
		p.busy[i] = false
		if err == nil {
			if t.Reply == "" {
				t.Reply = "..."
			}
			p.chats[i] = append(p.chats[i], *t)
		}
		s.mu.Unlock()
		if err != nil {
			p.send(errMsg("(전송 실패: %s)", friendlyError(err)))
			return
		}
		p.send(Msg{T: "dm", N: n, Text: t.Reply, Time: fmtClock(now())})
	}()
}

func playerNames(ps []*coopPlayer) []string {
	var n []string
	for _, p := range ps {
		n = append(n, p.name)
	}
	return n
}

func (s *coopServer) evidence(p *coopPlayer, n int, share bool) {
	var o outbox
	s.mu.Lock()
	defer func() { s.mu.Unlock(); o.flush() }()
	if s.state != "playing" || n < 1 || n > len(s.scen.Evidence) {
		return
	}
	if owner := s.owners[n-1]; owner != p {
		o.to(p, errMsg("%d번 자료는 %s님 권한입니다. 공유해 달라고 부탁하세요.", n, owner.name))
		return
	}
	e := s.scen.Evidence[n-1]
	m := Msg{T: "evidence", N: n, Title: e.Title, Text: e.Content}
	if !share {
		o.to(p, m)
		return
	}
	m.From = p.name
	s.all(&o, m)
}

func (s *coopServer) vote(p *coopPlayer, n int, reason string) {
	var o outbox
	s.mu.Lock()
	defer func() { s.mu.Unlock(); o.flush() }()
	if s.state != "playing" || n < 1 || n > len(s.scen.Suspects) {
		return
	}
	p.vote, p.reason = n, reason
	on := s.online()
	voted := 0
	for _, q := range on {
		if q.vote > 0 {
			voted++
		}
	}
	s.all(&o, sysMsg("%s님이 %s에게 투표했습니다. (%d/%d)", p.name, s.scen.Suspects[n-1].Name, voted, len(on)))
	if voted < len(on) {
		return
	}

	tally := map[int]int{}
	for _, q := range on {
		tally[q.vote]++
	}
	top, best := 0, 0
	for k, v := range tally {
		if v > best {
			top, best = k, v
		}
	}
	if best*2 <= len(on) {
		var parts []string
		for k, v := range tally {
			parts = append(parts, fmt.Sprintf("%s %d표", s.scen.Suspects[k-1].Name, v))
		}
		sort.Strings(parts)
		s.all(&o, sysMsg("의견이 갈렸습니다 (%s). 과반이 나와야 보고할 수 있어요. 의논하고 다시 투표하세요.", strings.Join(parts, ", ")))
		for _, q := range on {
			q.vote, q.reason = 0, ""
		}
		return
	}
	var reasons []string
	for _, q := range on {
		if q.vote == top && q.reason != "" {
			reasons = append(reasons, q.name+": "+q.reason)
		}
	}
	s.state = "over"
	s.all(&o, sysMsg("팀 결론: %s. %s에게 보고합니다...", s.scen.Suspects[top-1].Name, s.scen.BossName))
	go s.finish(top-1, strings.Join(reasons, "\n"))
}

func (s *coopServer) truth(e *CoopEnd) {
	sc := s.scen
	e.Culprit, e.Motive, e.Method, e.Key, e.Timeline = sc.Culprit, sc.Motive, sc.Method, sc.KeyContradiction, sc.Timeline
	for _, x := range sc.Suspects {
		if x.Name != sc.Culprit {
			e.Secrets = append(e.Secrets, x.Name+": "+x.Secret)
		}
	}
}

func (s *coopServer) finish(accused int, reasons string) {
	e := &CoopEnd{Accused: s.scen.Suspects[accused].Name, Win: accused == s.scen.CulpritIndex()}
	if e.Win {
		if v, err := JudgeReasoning(ctx, llm, s.scen, reasons); err == nil {
			e.Grade, e.Feedback = v.Grade, v.Feedback
		}
	} else {
		e.Feedback = "...이 사람 아니라는데요? 내일 아침에 다 같이 얘기 좀 합시다."
	}
	var o outbox
	s.mu.Lock()
	s.truth(e)
	s.all(&o, Msg{T: "end", End: e})
	s.mu.Unlock()
	o.flush()
}

func (s *coopServer) tick() {
	var o outbox
	s.mu.Lock()
	defer func() { s.mu.Unlock(); o.flush() }()
	if s.state != "playing" || s.practice {
		return
	}
	for _, text := range duePressure(s.started, s.deadline, s.fired) {
		s.all(&o, Msg{T: "boss", From: s.scen.BossName, Text: text, Time: fmtClock(now())})
	}
	if !now().Before(s.deadline) {
		s.state = "over"
		e := &CoopEnd{Timeout: true, Feedback: "퇴근 시간이네요. 결국 못 찾았군요... 이번 분기 팀 평가는 기대하지 마세요."}
		s.truth(e)
		s.all(&o, Msg{T: "end", End: e})
	}
}
