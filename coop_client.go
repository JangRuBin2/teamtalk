package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type chatLine struct {
	Time, From, Text string
	Color            string
}

type coopClient struct {
	conn    net.Conn
	name    string
	host    bool
	room    string
	info    *CoopInfo
	cur     int // 0 = #조사팀, 1..n = DM
	team    []chatLine
	dms     [][]chatLine
	evi     map[int]Msg
	players []string
}

func playCoop() {
	clearScreen()
	fmt.Println(cBold + "같이하기" + cReset + cDim + "  최대 4명이 함께 사내 추리 사건을 풉니다" + cReset)
	fmt.Println()
	fmt.Println("  1. 방 만들기   " + cDim + "접속 코드 필요. AI 호출은 방장 쪽에서 합니다" + cReset)
	fmt.Println("  2. 방 참가     " + cDim + "초대 코드만 있으면 됩니다" + cReset)
	fmt.Printf("\n  %s0. 돌아가기%s\n\n", cDim, cReset)
	c, ok := prompt("> ")
	if !ok || (c != "1" && c != "2") {
		return
	}
	if relayEndpoint() == "" {
		fmt.Println(cRed + "\n같이하기는 중계 서버가 연결된 버전에서만 할 수 있습니다." + cReset)
		waitEnter("Enter")
		return
	}
	if c == "1" {
		hostCoop()
	} else {
		joinCoop()
	}
}

func askNickname() (string, bool) {
	n, ok := prompt(fmt.Sprintf("닉네임 [%s]: ", or(cfg.Nickname, "익명")))
	if !ok {
		return "", false
	}
	n = or(strings.TrimSpace(n), or(cfg.Nickname, "익명"))
	if n != cfg.Nickname {
		cfg.Nickname = n
		saveConfig()
	}
	return n, true
}

func hostCoop() {
	if !ensureCredentials() {
		return
	}
	if cfg.AccessCode == "" { // a personal API key still needs a code to open rooms
		c, ok := prompt("방을 만들려면 중계 서버 접속 코드가 필요합니다: ")
		if !ok || c == "" {
			return
		}
		cfg.AccessCode = c
		saveConfig()
	}
	name, ok := askNickname()
	if !ok {
		return
	}
	stop := spinner("방 만드는 중")
	srv := newCoopServer()
	code, closeRoom, err := hostRoom(srv)
	stop()
	if err != nil {
		srv.close()
		fmt.Println(cRed + err.Error() + cReset)
		waitEnter("Enter")
		return
	}
	defer func() { closeRoom(); srv.close() }()
	ours, theirs := net.Pipe()
	go srv.serve(theirs)
	runCoopClient(ours, name, code)
}

func joinCoop() {
	code, ok := prompt("초대 코드: ")
	code = strings.ToUpper(strings.TrimSpace(code))
	if !ok || code == "" {
		return
	}
	name, ok := askNickname()
	if !ok {
		return
	}
	stop := spinner("방에 들어가는 중")
	conn, err := joinRoom(code)
	stop()
	if err != nil {
		fmt.Println(cRed + err.Error() + cReset)
		waitEnter("Enter")
		return
	}
	runCoopClient(conn, name, code)
}

func runCoopClient(conn net.Conn, name, room string) {
	defer conn.Close()
	c := &coopClient{conn: conn, name: name, room: room, evi: map[int]Msg{}}
	c.send(Msg{T: "hello", Name: name})

	netCh := make(chan Msg, 128)
	go func() {
		defer close(netCh)
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			var m Msg
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				netCh <- m
			}
		}
	}()

	clearScreen()
	c.lobbyScreen()
	for {
		select {
		case m, ok := <-netCh:
			if !ok {
				c.async(func() { fmt.Println(cRed + "\n방과의 연결이 끊어졌습니다." + cReset) })
				waitEnter("Enter를 누르면 게임 메뉴로 돌아갑니다.")
				return
			}
			if c.onNet(m) {
				return
			}
		case line, ok := <-inputCh:
			if !ok || c.onInput(line) {
				return
			}
			c.prompt()
		}
	}
}

func (c *coopClient) send(m Msg) {
	b, _ := json.Marshal(m)
	c.conn.Write(append(b, '\n'))
}

func (c *coopClient) prompt() {
	where := cBold + "#조사팀" + cReset
	if c.cur > 0 && c.info != nil {
		s := c.info.Suspects[c.cur-1]
		where = colorFor(c.cur-1) + "→ " + s.Name + cReset
	}
	fmt.Printf("%s[%s]%s %s %s나%s > ", cDim, fmtClock(now()), cReset, where, cBold, cReset)
}

// async prints something that arrived from the network above the input line.
func (c *coopClient) async(f func()) {
	fmt.Print("\r\x1b[2K")
	f()
	c.prompt()
}

func (c *coopClient) lobbyScreen() {
	fmt.Printf("%s같이하기%s  %s\n\n", cBold, cReset, cDim+workStatus()+cReset)
	fmt.Printf("  초대 코드  %s%s%s\n", cBold+cYellow, c.room, cReset)
	sysLine("  동료에게 이 코드를 알려주세요. 메뉴의 5. 같이하기 → 2. 방 참가에서 입력하면 됩니다.")
	fmt.Println()
	sysLine("여기서 입력하면 #조사팀 채팅으로 갑니다. 방장이 /start 하면 사건이 시작됩니다. (/q 나가기)")
	c.prompt()
}

func (c *coopClient) remaining() string {
	if c.info.Practice {
		return "연습 모드"
	}
	return "마감까지 " + fmtDur(c.info.Deadline.Sub(now()))
}

func (c *coopClient) header() {
	where, hint := "#조사팀", "1~5 DM · /e 자료 · /vote 투표 · /help"
	if c.cur > 0 {
		s := c.info.Suspects[c.cur-1]
		where, hint = fmt.Sprintf("DM: %s %s (%s)", s.Name, s.Title, s.Dept), "0 팀 채팅 · 1~5 DM 이동 · /e 자료 · /help"
	}
	fmt.Printf("\n%s── %s ── %s  %s ── 방 %s ── %s ──%s\n", cDim, where, fmtClock(now()), c.remaining(), c.room, hint, cReset)
}

func (c *coopClient) redraw() {
	if c.info == nil {
		c.lobbyScreen()
		return
	}
	c.header()
	lines := c.team
	if c.cur > 0 {
		lines = c.dms[c.cur-1]
	} else {
		printBubble(fmtClock(c.info.Started), cRed, c.info.Boss, c.info.Briefing)
	}
	from := max(0, len(lines)-12)
	if from > 0 {
		sysLine("... 이전 메시지 %d개 (/log 로 전체 보기)", from)
	}
	for _, l := range lines[from:] {
		printBubble(l.Time, l.Color, l.From, l.Text)
	}
}

func (c *coopClient) switchTo(n int) {
	c.cur = n
	clearScreen()
	c.redraw()
}

func (c *coopClient) showCase() {
	in := c.info
	clearScreen()
	fmt.Printf("%s%s%s 사내 메신저  |  #조사팀 (%s)\n\n", cBold, in.Company, cReset, strings.Join(c.players, ", "))
	printBubble(fmtClock(in.Started), cRed, in.Boss, in.Briefing)
	fmt.Println()
	sysLine("── 사건: %s ──", in.Incident)
	if in.Practice {
		sysLine("연습 모드: 시간 제한이 없습니다.")
	} else {
		sysLine("실제 시간으로 %s 퇴근 전까지 팀이 함께 범인을 찾아야 합니다.", fmtClock(in.Deadline))
	}
	sysLine("동료 AI와의 DM은 사람마다 따로입니다. 알아낸 건 #조사팀(0)에서 공유하세요.")
	sysLine("자료는 사람마다 나눠서 배정됐습니다. 내 자료는 /e 번호로 보고 /share 번호로 팀에 공유하세요.")
	sysLine("범인은 /vote 번호 근거 로 투표합니다. 접속한 전원이 투표해서 과반이 나오면 보고됩니다.")
	fmt.Println()
	c.printSuspects()
	c.printEvidence()
	c.prompt()
}

func (c *coopClient) printSuspects() {
	sysLine("용의자 (번호로 DM)")
	for i, s := range c.info.Suspects {
		fmt.Printf("  %d. %s%s %s%s %s- %s%s\n", i+1, colorFor(i), s.Name, s.Title, cReset, cDim, s.Dept, cReset)
	}
}

func (c *coopClient) printEvidence() {
	sysLine("자료")
	for i, e := range c.info.Evidence {
		who := cDim + e.Owner + "님 권한" + cReset
		if e.Mine {
			who = cGreen + "내 권한" + cReset
		}
		seen := ""
		if _, ok := c.evi[i+1]; ok {
			seen = cDim + " · 열람함" + cReset
		}
		fmt.Printf("  %d. %s  (%s%s)\n", i+1, e.Title, who, seen)
	}
}

func printEvidenceBox(m Msg) {
	title := m.Title
	if m.From != "" {
		title += cReset + cDim + "  (" + m.From + "님이 공유)"
	}
	fmt.Printf("\n%s┌─ %d. %s%s\n", cBold, m.N, title, cReset)
	for _, l := range strings.Split(strings.TrimSpace(m.Text), "\n") {
		fmt.Printf("%s│%s %s\n", cDim, cReset, l)
	}
	fmt.Printf("%s└─%s\n", cDim, cReset)
}

func (c *coopClient) teamColor(name string) string {
	if name == c.name {
		return cBold
	}
	for i, p := range c.players {
		if p == name {
			return npcColors[(i+3)%len(npcColors)]
		}
	}
	return cCyan
}

// onNet handles one server message; it returns true when the game is over.
func (c *coopClient) onNet(m Msg) bool {
	switch m.T {
	case "welcome":
		c.host = m.Host
		if c.host {
			c.async(func() { sysLine("방장입니다. 다 모이면 /start 로 시작하세요.") })
		}
	case "lobby":
		c.players = m.Players
		c.async(func() {
			sysLine("참가자: %s (%d/%d)", strings.Join(m.Players, ", "), len(m.Players), coopMaxPlayers)
		})
	case "system":
		c.async(func() { sysLine("%s", m.Text) })
	case "error":
		c.async(func() { fmt.Println(cRed + m.Text + cReset) })
	case "game":
		first := c.info == nil
		c.info = m.Game
		if len(c.dms) != len(c.info.Suspects) {
			c.dms = make([][]chatLine, len(c.info.Suspects))
		}
		if first {
			fmt.Print("\r\x1b[2K")
			c.showCase()
		}
	case "team":
		l := chatLine{m.Time, m.From, m.Text, c.teamColor(m.From)}
		c.team = append(c.team, l)
		if m.From == c.name && c.cur == 0 {
			return false // already echoed locally
		}
		c.async(func() {
			if c.cur != 0 {
				fmt.Printf("%s[#조사팀]%s ", cYellow, cReset)
			}
			printBubble(l.Time, l.Color, l.From, l.Text)
		})
	case "dm":
		if c.info == nil || m.N < 1 || m.N > len(c.dms) {
			return false
		}
		s := c.info.Suspects[m.N-1]
		l := chatLine{m.Time, s.Name, m.Text, colorFor(m.N - 1)}
		c.dms[m.N-1] = append(c.dms[m.N-1], l)
		c.async(func() {
			if c.cur != m.N {
				fmt.Printf("%s[DM %d]%s ", cYellow, m.N, cReset)
			}
			printBubble(l.Time, l.Color, l.From, l.Text)
		})
	case "evidence":
		c.evi[m.N] = m
		c.async(func() { printEvidenceBox(m) })
	case "boss":
		c.async(func() {
			fmt.Printf("%s[알림]%s ", cYellow, cReset)
			printBubble(m.Time, cRed, m.From, m.Text)
		})
	case "end":
		fmt.Print("\r\x1b[2K")
		c.showEnd(m.End)
		waitEnter("Enter를 누르면 게임 메뉴로 돌아갑니다.")
		return true
	}
	return false
}

func (c *coopClient) showEnd(e *CoopEnd) {
	fmt.Println()
	boss := "팀장"
	if c.info != nil {
		boss = c.info.Boss
	}
	switch {
	case e.Timeout:
		fmt.Printf("%s%s시간 초과! 퇴근 시간이 지났습니다.%s\n", cRed, cBold, cReset)
	case e.Win:
		fmt.Printf("%s%s정답! 범인은 %s였습니다.%s", cGreen, cBold, e.Culprit, cReset)
		if e.Grade != "" {
			fmt.Printf("  팀 추리 등급: %s%s%s", cBold, e.Grade, cReset)
		}
		fmt.Println()
	default:
		fmt.Printf("%s%s오답... %s님은 억울하게 시말서를 쓰게 됐습니다.%s\n", cRed, cBold, e.Accused, cReset)
	}
	if e.Feedback != "" {
		printBubble(fmtClock(now()), cRed, boss, e.Feedback)
	}
	fmt.Printf("\n%s── 사건의 진실 ──%s\n", cBold, cReset)
	fmt.Printf("범인: %s\n동기: %s\n수법: %s\n결정적 모순: %s\n\n", e.Culprit, e.Motive, e.Method, e.Key)
	for _, t := range e.Timeline {
		fmt.Printf("  %s- %s%s\n", cDim, t, cReset)
	}
	fmt.Println()
	sysLine("다른 사람들의 비밀")
	for _, s := range e.Secrets {
		fmt.Printf("  %s\n", s)
	}
	fmt.Println()
}

func (c *coopClient) printHelp() {
	sysLine(`명령어
  0, /t          #조사팀 채팅으로
  1~5            해당 용의자와 DM (내 대화는 나만 봅니다)
  /e [번호]      자료 목록 / 내 권한 자료 열람
  /share 번호    내 자료를 팀에 공유
  /vote 번호 근거  범인 투표 (전원 투표 후 과반이면 보고)
  /who           참가자 목록
  /log           현재 채널 전체 보기
  /b             보스키 (Enter로 복귀)
  /start         (방장) 사건 시작
  /q             방 나가기 (방장이 나가면 방이 닫힙니다)`)
}

// onInput handles one line typed by the player; it returns true to leave.
func (c *coopClient) onInput(line string) bool {
	if line == "" {
		return false
	}
	if metaCommand(line, c.redraw) {
		return false
	}
	if c.info != nil {
		if line == "0" {
			c.switchTo(0)
			return false
		}
		if n, err := strconv.Atoi(line); err == nil {
			if n >= 1 && n <= len(c.info.Suspects) {
				c.switchTo(n)
			} else {
				sysLine("0~%d 사이 번호를 입력하세요.", len(c.info.Suspects))
			}
			return false
		}
	}
	if !strings.HasPrefix(line, "/") {
		t := fmtClock(now())
		if c.cur == 0 {
			c.send(Msg{T: "team", Text: line})
			fmt.Print("\x1b[1A\r\x1b[2K") // replace the typed line with the bubble
			printBubble(t, cBold, c.name, line)
			return false
		}
		c.dms[c.cur-1] = append(c.dms[c.cur-1], chatLine{t, "나", line, cBold})
		c.send(Msg{T: "dm", N: c.cur, Text: line})
		sysLine("(%s님이 입력 중...)", c.info.Suspects[c.cur-1].Name)
		return false
	}

	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	switch strings.ToLower(cmd) {
	case "/help", "/h", "/?":
		c.printHelp()
	case "/start":
		c.send(Msg{T: "start"})
	case "/t", "/team":
		if c.info != nil {
			c.switchTo(0)
		}
	case "/who":
		sysLine("참가자: %s", strings.Join(c.players, ", "))
	case "/log":
		lines := c.team
		if c.cur > 0 {
			lines = c.dms[c.cur-1]
		}
		for _, l := range lines {
			printBubble(l.Time, l.Color, l.From, l.Text)
		}
	case "/e", "/evi":
		if c.info == nil {
			return false
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > len(c.info.Evidence) {
			c.printEvidence()
			return false
		}
		if m, ok := c.evi[n]; ok {
			printEvidenceBox(m)
		} else if c.info.Evidence[n-1].Mine {
			c.send(Msg{T: "evi", N: n})
		} else {
			sysLine("%d번 자료는 %s님 권한입니다. #조사팀에서 공유해 달라고 하세요.", n, c.info.Evidence[n-1].Owner)
		}
	case "/share":
		n, err := strconv.Atoi(arg)
		if c.info == nil || err != nil {
			sysLine("사용법: /share 자료번호")
			return false
		}
		c.send(Msg{T: "share", N: n})
	case "/vote":
		num, reason, _ := strings.Cut(arg, " ")
		n, err := strconv.Atoi(num)
		if c.info == nil || err != nil || n < 1 || n > len(c.info.Suspects) {
			sysLine("사용법: /vote 용의자번호 근거  (예: /vote 3 출입기록과 알리바이가 안 맞음)")
			return false
		}
		c.send(Msg{T: "vote", N: n, Text: strings.TrimSpace(reason)})
	case "/q", "/quit":
		label := "방에서 나갈까요? [y/N] "
		if c.host {
			label = "방장이 나가면 방이 닫히고 모두의 게임이 끝납니다. 나갈까요? [y/N] "
		}
		return yes(label, false)
	default:
		sysLine("알 수 없는 명령어입니다. /help 를 입력하세요.")
	}
	return false
}
