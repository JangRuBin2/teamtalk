package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const whoSave = "save.json"

var pressureTexts = []string{
	"진행 상황 어때요? 감은 좀 잡혔어요?",
	"대표님이 아까 이 건 물어보셨어요. 오늘 안에 꼭 정리합시다.",
	"퇴근까지 %s 남았어요. 아무나 찍으면 안 되는 거 알죠?",
	"30분 남았어요. 퇴근 전에 보고 들어갑니다.",
}

// Game is the save state of the mystery game. Time is the real clock: the
// case starts when it is generated and must be solved by Deadline.
type Game struct {
	Scenario *Scenario `json:"scenario"`
	Started  time.Time `json:"started"`
	Deadline time.Time `json:"deadline"`
	Practice bool      `json:"practice"` // no deadline
	Current  int       `json:"current"`  // -1 = #general
	Chats    [][]Turn  `json:"chats"`
	Viewed   []bool    `json:"viewed"`
	Memos    []string  `json:"memos"`
	Fired    []bool    `json:"fired"`
}

func whodunitStatus() string {
	var g Game
	if readJSON(whoSave, &g) != nil || g.Scenario == nil {
		return ""
	}
	if g.Practice {
		return cYellow + "[진행 중 · 연습]" + cReset
	}
	return fmt.Sprintf("%s[진행 중 · 마감 %s]%s", cYellow, fmtClock(g.Deadline), cReset)
}

func playWhodunit() {
	g := &Game{}
	if readJSON(whoSave, g) != nil || g.Scenario == nil || !yes(fmt.Sprintf("진행 중인 사건이 있습니다 (%s). 이어서 할까요? [Y/n] ", g.Scenario.IncidentTitle), true) {
		g = newCase()
		if g == nil {
			return
		}
		showBriefing(g)
	} else {
		clearScreen()
		redrawWho(g)
	}
	runWho(g)
}

func newCase() *Game {
	start := now()
	_, off := workDay(start)
	g := &Game{Started: start, Deadline: off, Current: -1}
	deadline := fmt.Sprintf("오늘 %s 퇴근 전", cfg.WorkEnd)
	if off.Sub(start) < 30*time.Minute {
		if !yes(fmt.Sprintf("퇴근 시간(%s)까지 30분도 안 남았어요. 시간 제한 없는 연습 모드로 할까요? [Y/n] ", cfg.WorkEnd), true) {
			return nil
		}
		g.Practice = true
		deadline = "오늘 안"
	}
	for {
		stop := spinner("사건 파일을 받는 중 (30초~1분 정도 걸려요)")
		s, err := GenerateScenario(ctx, llm, fmtClock(start), deadline)
		stop()
		if err == nil {
			g.Scenario = s
			g.Chats = make([][]Turn, len(s.Suspects))
			g.Viewed = make([]bool, len(s.Evidence))
			g.Fired = make([]bool, len(pressureTexts))
			saveWho(g)
			return g
		}
		fmt.Println(cRed + "접속 실패: " + friendlyError(err) + cReset)
		if !yes("다시 시도할까요? [Y/n] ", true) {
			return nil
		}
	}
}

func saveWho(g *Game) {
	if err := writeJSON(whoSave, g); err != nil {
		sysLine("(저장 실패: %v)", err)
	}
}

func (g *Game) timeUp() bool { return !g.Practice && !now().Before(g.Deadline) }

func (g *Game) remaining() string {
	if g.Practice {
		return "연습 모드"
	}
	return "마감까지 " + fmtDur(g.Deadline.Sub(now()))
}

func showBriefing(g *Game) {
	clearScreen()
	s := g.Scenario
	fmt.Printf("%s%s%s 사내 메신저  |  #general\n\n", cBold, s.Company, cReset)
	printBubble(fmtClock(g.Started), cRed, s.BossName, s.Briefing)
	fmt.Println()
	sysLine("── 사건: %s ──", s.IncidentTitle)
	if g.Practice {
		sysLine("연습 모드: 시간 제한이 없습니다. 범인을 찾으면 /accuse 로 지목하세요.")
	} else {
		sysLine("실제 시간으로 %s 퇴근 전까지 범인을 찾아 /accuse 로 지목하세요.", fmtClock(g.Deadline))
	}
	sysLine("1~%d: 동료 DM · 0: 메인(#general) · /e: 증거 · /help: 도움말 · /b: 보스키", len(s.Suspects))
	fmt.Println()
	printList(g)
}

func printWhoHelp() {
	sysLine(`명령어
  1~5, /dm 이름   해당 동료와 DM 열기 (이후 그냥 입력하면 메시지 전송)
  0, /g          메인(#general)으로 돌아가기
  /ls            동료 목록
  /e [번호]      증거 목록 / 열람
  /memo [내용]   메모 보기 / 추가
  /log           현재 DM 대화 전체 보기
  /accuse        범인 지목 (게임 종료)
  /b             보스키 (가짜 빌드 화면, Enter로 복귀)
  /clear         화면 정리
  /q             저장하고 게임 메뉴로`)
}

func printList(g *Game) {
	sysLine("동료 목록")
	for i, p := range g.Scenario.Suspects {
		tag := ""
		if n := len(g.Chats[i]); n > 0 {
			tag = fmt.Sprintf(" (대화 %d)", n)
		}
		fmt.Printf("  %d. %s%s %s%s %s- %s%s%s\n", i+1, colorFor(i), p.Name, p.Title, cReset, cDim, p.Dept, tag, cReset)
	}
}

func headerWho(g *Game) {
	where := "#general"
	hint := "1~5 DM · /e 증거 · /help"
	if g.Current >= 0 {
		p := g.Scenario.Suspects[g.Current]
		where = fmt.Sprintf("DM: %s %s (%s)", p.Name, p.Title, p.Dept)
		hint = "0 메인 · 1~5 DM 이동 · /e 증거 · /help"
	}
	fmt.Printf("\n%s── %s ── %s  %s ── %s ──%s\n", cDim, where, fmtClock(now()), g.remaining(), hint, cReset)
}

func redrawWho(g *Game) {
	headerWho(g)
	if g.Current < 0 {
		printBubble(fmtClock(g.Started), cRed, g.Scenario.BossName, g.Scenario.Briefing)
		return
	}
	printLog(g, 6)
}

func printLog(g *Game, last int) {
	chat := g.Chats[g.Current]
	if last > 0 && len(chat) > last {
		sysLine("... 이전 대화 %d개 (/log 로 전체 보기)", len(chat)-last)
		chat = chat[len(chat)-last:]
	}
	p := g.Scenario.Suspects[g.Current]
	for _, t := range chat {
		printBubble(t.Time, cBold, "나", t.User)
		printBubble(t.Time, colorFor(g.Current), p.Name, t.Reply)
	}
}

// checkEvents shows the boss's reminders as real time passes.
func checkEvents(g *Game) {
	if g.Practice {
		return
	}
	for _, text := range duePressure(g.Started, g.Deadline, g.Fired) {
		fmt.Printf("%s[알림] #general%s ", cYellow, cReset)
		printBubble(fmtClock(now()), cRed, g.Scenario.BossName, text)
	}
}

// duePressure marks and returns the boss reminders that are due now. They
// fire at 1/4, 1/2 and 3/4 of the case and 30 minutes before the deadline.
func duePressure(started, deadline time.Time, fired []bool) []string {
	t := now()
	span := deadline.Sub(started)
	at := []time.Time{
		started.Add(span / 4),
		started.Add(span / 2),
		started.Add(span * 3 / 4),
		deadline.Add(-30 * time.Minute),
	}
	var due []string
	for i, e := range at {
		if fired[i] || t.Before(e) {
			continue
		}
		fired[i] = true
		if t.Sub(e) > time.Hour || !e.After(started) { // stale, or the case started too late for it
			continue
		}
		text := pressureTexts[i]
		if strings.Contains(text, "%s") {
			text = fmt.Sprintf(text, fmtDur(deadline.Sub(t)))
		}
		due = append(due, text)
	}
	return due
}

func findSuspect(g *Game, q string) int {
	q = strings.TrimSpace(q)
	if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= len(g.Scenario.Suspects) {
		return n - 1
	}
	if q == "" {
		return -1
	}
	for i, p := range g.Scenario.Suspects {
		if strings.Contains(p.Name, q) || strings.Contains(q, p.Name) {
			return i
		}
	}
	return -1
}

func openChannel(g *Game, i int) {
	g.Current = i
	clearScreen()
	redrawWho(g)
}

func runWho(g *Game) {
	for {
		if g.timeUp() {
			timeUp(g)
			return
		}
		fmt.Printf("%s[%s]%s %s나%s > ", cDim, fmtClock(now()), cReset, cBold, cReset)
		line, ok := readLine()
		if !ok {
			saveWho(g)
			return
		}
		if g.timeUp() {
			timeUp(g)
			return
		}
		if line == "" {
			continue
		}
		if handleWho(g, line) {
			return
		}
		checkEvents(g)
		saveWho(g)
	}
}

// handleWho processes one input line; it returns true when leaving the game.
func handleWho(g *Game, line string) bool {
	if metaCommand(line, func() { redrawWho(g) }) {
		return false
	}
	if line == "0" {
		openChannel(g, -1)
		return false
	}
	if _, err := strconv.Atoi(line); err == nil {
		if i := findSuspect(g, line); i >= 0 {
			openChannel(g, i)
		} else {
			sysLine("0~%d 사이 번호를 입력하세요.", len(g.Scenario.Suspects))
		}
		return false
	}
	if !strings.HasPrefix(line, "/") {
		if g.Current < 0 {
			sysLine("#general은 팀장님이 보고 있어요. 숫자(1~%d)로 동료 DM을 먼저 여세요.", len(g.Scenario.Suspects))
			return false
		}
		sendDM(g, line)
		return false
	}

	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	switch strings.ToLower(cmd) {
	case "/help", "/h", "/?":
		printWhoHelp()
	case "/dm":
		if i := findSuspect(g, arg); i >= 0 {
			openChannel(g, i)
		} else {
			sysLine("그런 동료가 없습니다. /ls 로 목록을 확인하세요.")
		}
	case "/g", "/general":
		openChannel(g, -1)
	case "/ls", "/list":
		printList(g)
	case "/e", "/evi", "/evidence":
		evidence(g, arg)
	case "/memo", "/m":
		if arg == "" {
			if len(g.Memos) == 0 {
				sysLine("메모가 없습니다. /memo 내용 으로 추가하세요.")
			}
			for i, m := range g.Memos {
				fmt.Printf("  %d. %s\n", i+1, m)
			}
		} else {
			g.Memos = append(g.Memos, fmt.Sprintf("[%s] %s", fmtClock(now()), arg))
			sysLine("메모 저장됨.")
		}
	case "/log":
		if g.Current < 0 {
			sysLine("DM을 먼저 여세요.")
		} else {
			printLog(g, 0)
		}
	case "/accuse":
		return accuse(g)
	case "/q", "/quit", "/exit":
		saveWho(g)
		return true
	default:
		sysLine("알 수 없는 명령어입니다. /help 를 입력하세요.")
	}
	return false
}

func sendDM(g *Game, text string) {
	i := g.Current
	p := g.Scenario.Suspects[i]
	stop := spinner(fmt.Sprintf("%s님이 입력 중", p.Name))
	turn, err := AskNPC(g.Scenario, i, g.Chats[i], fmtClock(now()), text)
	stop()
	if err != nil {
		sysLine("(전송 실패: %s)", friendlyError(err))
		return
	}
	g.Chats[i] = append(g.Chats[i], *turn)
	printBubble(fmtClock(now()), colorFor(i), p.Name, turn.Reply)
}

func evidence(g *Game, arg string) {
	ev := g.Scenario.Evidence
	n, err := strconv.Atoi(arg)
	if arg == "" || err != nil || n < 1 || n > len(ev) {
		sysLine("열람 가능한 자료 (/e 번호)")
		for i, e := range ev {
			mark := ""
			if g.Viewed[i] {
				mark = cDim + " (열람함)" + cReset
			}
			fmt.Printf("  %d. %s%s\n", i+1, e.Title, mark)
		}
		return
	}
	i := n - 1
	g.Viewed[i] = true
	fmt.Printf("\n%s┌─ %s%s\n", cBold, ev[i].Title, cReset)
	for _, l := range strings.Split(strings.TrimSpace(ev[i].Content), "\n") {
		fmt.Printf("%s│%s %s\n", cDim, cReset, l)
	}
	fmt.Printf("%s└─%s\n", cDim, cReset)
}

func accuse(g *Game) bool {
	s := g.Scenario
	printList(g)
	who, ok := prompt("범인은? (번호 또는 이름, 취소는 Enter): ")
	i := findSuspect(g, who)
	if !ok || i < 0 {
		sysLine("지목을 취소했습니다.")
		return false
	}
	reason, ok := prompt("근거를 적어주세요: ")
	if !ok {
		return false
	}
	if !yes(fmt.Sprintf("정말 %s %s님을 범인으로 보고할까요? 되돌릴 수 없습니다. [y/N] ", s.Suspects[i].Name, s.Suspects[i].Title), false) {
		sysLine("지목을 취소했습니다.")
		return false
	}
	removeFile(whoSave)
	fmt.Println()
	if i == s.CulpritIndex() {
		stop := spinner(s.BossName + "님이 보고서를 읽는 중")
		v, err := JudgeReasoning(ctx, llm, s, reason)
		stop()
		fmt.Printf("%s%s정답! 범인은 %s %s였습니다.%s\n", cGreen, cBold, s.Culprit, s.Suspects[i].Title, cReset)
		if err == nil {
			printBubble(fmtClock(now()), cRed, s.BossName, v.Feedback)
			left := ""
			if !g.Practice {
				left = "   마감까지 남은 시간: " + fmtDur(g.Deadline.Sub(now()))
			}
			fmt.Printf("\n추리 등급: %s%s%s%s\n", cBold, v.Grade, cReset, left)
		}
	} else {
		fmt.Printf("%s%s오답... %s님은 억울하게 시말서를 쓰게 됐습니다.%s\n", cRed, cBold, s.Suspects[i].Name, cReset)
		printBubble(fmtClock(now()), cRed, s.BossName, "...이 사람 아니라는데요? 내일 아침에 저랑 따로 얘기 좀 합시다.")
	}
	reveal(s)
	waitEnter("Enter를 누르면 게임 메뉴로 돌아갑니다.")
	return true
}

func timeUp(g *Game) {
	s := g.Scenario
	removeFile(whoSave)
	fmt.Println()
	printBubble(fmtClock(g.Deadline), cRed, s.BossName, "퇴근 시간이네요. 결국 못 찾았군요... 대표님께는 제가 잘 말씀드려볼게요. 근데 이번 분기 평가는 기대하지 마세요.")
	fmt.Printf("\n%s%s시간 초과! 퇴근 시간이 지났습니다.%s\n", cRed, cBold, cReset)
	reveal(s)
	waitEnter("Enter를 누르면 게임 메뉴로 돌아갑니다.")
}

func reveal(s *Scenario) {
	fmt.Printf("\n%s── 사건의 진실 ──%s\n", cBold, cReset)
	fmt.Printf("범인: %s\n동기: %s\n수법: %s\n결정적 모순: %s\n\n", s.Culprit, s.Motive, s.Method, s.KeyContradiction)
	for _, t := range s.Timeline {
		fmt.Printf("  %s- %s%s\n", cDim, t, cReset)
	}
	fmt.Println()
	sysLine("다른 사람들의 비밀")
	for _, p := range s.Suspects {
		if p.Name != s.Culprit {
			fmt.Printf("  %s: %s\n", p.Name, p.Secret)
		}
	}
	fmt.Println()
}
