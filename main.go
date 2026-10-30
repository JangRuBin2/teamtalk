package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	startMin   = 9 * 60
	lunchStart = 12 * 60
	lunchEnd   = 13 * 60
	endMin     = 18 * 60
	costMsg    = 10 // minutes per DM message
	costEvi    = 20 // minutes to look up evidence the first time
)

type pressureEvent struct {
	At   int
	Text string
}

var pressureEvents = []pressureEvent{
	{11 * 60, "진행 상황 어때요? 점심 전에 감은 좀 잡혔으면 좋겠는데."},
	{14 * 60, "대표님이 아까 이 건 물어보셨어요. 오늘 안에 꼭 정리합시다."},
	{16 * 60, "두 시간 남았습니다. 아무나 찍으면 안 되는 거 알죠?"},
	{17*60 + 30, "30분 남았어요. 6시에 대표님 보고 들어갑니다."},
}

// Game is the whole save state.
type Game struct {
	Scenario *Scenario `json:"scenario"`
	Minutes  int       `json:"minutes"`
	Current  int       `json:"current"` // -1 = #general
	Chats    [][]Turn  `json:"chats"`
	Viewed   []bool    `json:"viewed"`
	Memos    []string  `json:"memos"`
	Fired    []bool    `json:"fired"`
}

var (
	stdin = bufio.NewReader(os.Stdin)
	llm   *LLM
	ctx   = context.Background()
)

func readLine() (string, bool) {
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimSpace(line), true
}

func prompt(label string) (string, bool) {
	fmt.Print(label)
	return readLine()
}

func exit(code int) {
	if pauseOnExit {
		fmt.Print(cDim + "\nEnter를 누르면 창이 닫힙니다." + cReset)
		readLine()
	}
	os.Exit(code)
}

func main() {
	enableANSI()
	key := loadAPIKey()
	model := envOr("TEAMTALK_MODEL", defaultModel)
	llm = NewLLM(key, model, envOr("TEAMTALK_NPC_MODEL", model))

	clearScreen()
	fmt.Println(cBold + "TeamTalk for Work" + cReset + cDim + "  v3.2.1 (build 20261030)" + cReset)
	fmt.Println()

	g := &Game{}
	if readJSON("save.json", g) == nil && g.Scenario != nil {
		ans, _ := prompt(fmt.Sprintf("저장된 업무가 있습니다 (%s, %s). 이어서 하시겠습니까? [Y/n] ", g.Scenario.IncidentTitle, fmtClock(g.Minutes)))
		if strings.HasPrefix(strings.ToLower(ans), "n") {
			g = nil
		}
	} else {
		g = nil
	}
	if g == nil {
		g = newGame()
		showBriefing(g)
	} else {
		clearScreen()
		redraw(g)
	}
	run(g)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadAPIKey() string {
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		return k
	}
	var cfg Config
	if readJSON("config.json", &cfg) == nil && cfg.APIKey != "" {
		return cfg.APIKey
	}
	return askAPIKey()
}

func askAPIKey() string {
	fmt.Println("처음 실행하셨네요. Anthropic API 키가 필요합니다. (https://console.anthropic.com)")
	for {
		k, ok := prompt("API 키 입력: ")
		if !ok {
			exit(1)
		}
		if strings.HasPrefix(k, "sk-") {
			if err := writeJSON("config.json", Config{APIKey: k}); err != nil {
				sysLine("키 저장 실패(이번 실행에만 사용): %v", err)
			}
			return k
		}
		fmt.Println("sk- 로 시작하는 키를 입력해 주세요.")
	}
}

func newGame() *Game {
	for {
		stop := spinner("서버에 접속하는 중 (사건 생성 중, 30초~1분 정도 걸려요)")
		s, err := GenerateScenario(ctx, llm)
		stop()
		if err == nil {
			g := &Game{
				Scenario: s,
				Minutes:  startMin,
				Current:  -1,
				Chats:    make([][]Turn, len(s.Suspects)),
				Viewed:   make([]bool, len(s.Evidence)),
				Fired:    make([]bool, len(pressureEvents)),
			}
			save(g)
			return g
		}
		fmt.Println(cRed + "접속 실패: " + friendlyError(err) + cReset)
		if ans, ok := prompt("다시 시도할까요? [Y/n] "); !ok || strings.HasPrefix(strings.ToLower(ans), "n") {
			exit(1)
		}
	}
}

func save(g *Game) {
	if err := writeJSON("save.json", g); err != nil {
		sysLine("(저장 실패: %v)", err)
	}
}

func showBriefing(g *Game) {
	clearScreen()
	s := g.Scenario
	fmt.Printf("%s%s%s 사내 메신저  |  #general\n\n", cBold, s.Company, cReset)
	printBubble(fmtClock(g.Minutes), cRed, s.BossName, s.Briefing)
	fmt.Println()
	sysLine("── 사건: %s ──", s.IncidentTitle)
	sysLine("18:00 퇴근 전까지 범인을 찾아 /accuse 로 지목하세요. 메시지 1개 = %d분, 증거 첫 열람 = %d분.", costMsg, costEvi)
	sysLine("도움말은 /help, 급할 땐 /b (보스키)")
	fmt.Println()
	printList(g)
}

func printHelp() {
	sysLine(`명령어
  1~5, /dm 이름   해당 동료와 DM 열기 (이후 그냥 입력하면 메시지 전송)
  /g             #general 로 돌아가기 (브리핑 다시 보기)
  /ls            동료 목록
  /e [번호]      증거 목록 / 열람
  /memo [내용]   메모 보기 / 추가
  /log           현재 DM 대화 다시 보기
  /accuse        범인 지목 (게임 종료)
  /b             보스키 (가짜 빌드 화면, Enter로 복귀)
  /clear         화면 정리
  /key           API 키 다시 설정
  /q             저장하고 종료`)
}

func printList(g *Game) {
	sysLine("동료 목록")
	for i, p := range g.Scenario.Suspects {
		n := len(g.Chats[i])
		tag := ""
		if n > 0 {
			tag = fmt.Sprintf(" (대화 %d)", n)
		}
		fmt.Printf("  %d. %s%s %s%s %s- %s%s%s\n", i+1, colorFor(i), p.Name, p.Title, cReset, cDim, p.Dept, tag, cReset)
	}
}

func header(g *Game) {
	left := endMin - g.Minutes
	where := "#general"
	if g.Current >= 0 {
		p := g.Scenario.Suspects[g.Current]
		where = fmt.Sprintf("DM: %s %s (%s)", p.Name, p.Title, p.Dept)
	}
	fmt.Printf("\n%s── %s ── %s  퇴근까지 %dh %02dm ──%s\n", cDim, where, fmtClock(g.Minutes), left/60, left%60, cReset)
}

func redraw(g *Game) {
	header(g)
	if g.Current < 0 {
		printBubble("09:00", cRed, g.Scenario.BossName, g.Scenario.Briefing)
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

// advance moves the clock and fires lunch / boss events.
func advance(g *Game, min int) {
	before := g.Minutes
	g.Minutes += min
	if before < lunchStart && g.Minutes >= lunchStart {
		g.Minutes += lunchEnd - lunchStart
		sysLine("\n[12:00] 점심시간입니다. 다들 밥 먹으러 나갔습니다... (13:00으로 이동)")
	}
	for i, e := range pressureEvents {
		if !g.Fired[i] && g.Minutes >= e.At && g.Minutes < endMin {
			g.Fired[i] = true
			if g.Minutes-e.At > 60 { // stale (skipped over by lunch)
				continue
			}
			fmt.Printf("%s[알림] #general%s ", cYellow, cReset)
			printBubble(fmtClock(e.At), cRed, g.Scenario.BossName, e.Text)
		}
	}
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

func run(g *Game) {
	for {
		if g.Minutes >= endMin {
			timeUp(g)
			return
		}
		fmt.Printf("%s[%s]%s %s나%s > ", cDim, fmtClock(g.Minutes), cReset, cBold, cReset)
		line, ok := readLine()
		if !ok {
			save(g)
			return
		}
		if line == "" {
			continue
		}
		if handleCommand(g, line) {
			return
		}
		save(g)
	}
}

// handleCommand processes one input line; it returns true when the game ends.
func handleCommand(g *Game, line string) bool {
	if _, err := strconv.Atoi(line); err == nil {
		if i := findSuspect(g, line); i >= 0 {
			g.Current = i
			clearScreen()
			redraw(g)
		} else {
			sysLine("1~%d 사이 번호를 입력하세요.", len(g.Scenario.Suspects))
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
		printHelp()
	case "/dm":
		if i := findSuspect(g, arg); i >= 0 {
			g.Current = i
			clearScreen()
			redraw(g)
		} else {
			sysLine("그런 동료가 없습니다. /ls 로 목록을 확인하세요.")
		}
	case "/g", "/general":
		g.Current = -1
		clearScreen()
		redraw(g)
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
			g.Memos = append(g.Memos, fmt.Sprintf("[%s] %s", fmtClock(g.Minutes), arg))
			sysLine("메모 저장됨.")
		}
	case "/log":
		if g.Current < 0 {
			sysLine("DM을 먼저 여세요.")
		} else {
			printLog(g, 0)
		}
	case "/clear", "/cls":
		clearScreen()
		redraw(g)
	case "/b", "/boss":
		bossKey(readLine)
		redraw(g)
	case "/key":
		k := askAPIKey()
		llm = NewLLM(k, llm.model, llm.npcModel)
	case "/accuse":
		return accuse(g)
	case "/q", "/quit", "/exit":
		save(g)
		sysLine("저장했습니다. 다음에 실행하면 이어서 할 수 있어요.")
		return true
	default:
		sysLine("알 수 없는 명령어입니다. /help 를 입력하세요.")
	}
	return false
}

func sendDM(g *Game, text string) {
	i := g.Current
	p := g.Scenario.Suspects[i]
	clock := fmtClock(g.Minutes)
	stop := spinner(fmt.Sprintf("%s님이 입력 중", p.Name))
	turn, err := AskNPC(ctx, llm, g.Scenario, i, g.Chats[i], clock, text)
	stop()
	if err != nil {
		sysLine("(전송 실패: %s — 시간은 흐르지 않았습니다)", friendlyError(err))
		return
	}
	g.Chats[i] = append(g.Chats[i], *turn)
	advance(g, costMsg)
	printBubble(fmtClock(g.Minutes), colorFor(i), p.Name, turn.Reply)
}

func evidence(g *Game, arg string) {
	ev := g.Scenario.Evidence
	n, err := strconv.Atoi(arg)
	if arg == "" || err != nil || n < 1 || n > len(ev) {
		sysLine("열람 가능한 자료 (/e 번호)")
		for i, e := range ev {
			mark := fmt.Sprintf("+%d분", costEvi)
			if g.Viewed[i] {
				mark = "열람함"
			}
			fmt.Printf("  %d. %s %s(%s)%s\n", i+1, e.Title, cDim, mark, cReset)
		}
		return
	}
	i := n - 1
	if !g.Viewed[i] {
		g.Viewed[i] = true
		sysLine("자료 요청 중... (%d분 소요)", costEvi)
		advance(g, costEvi)
	}
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
	conf, _ := prompt(fmt.Sprintf("정말 %s %s님을 범인으로 보고할까요? 되돌릴 수 없습니다. [y/N] ", s.Suspects[i].Name, s.Suspects[i].Title))
	if !strings.HasPrefix(strings.ToLower(conf), "y") {
		sysLine("지목을 취소했습니다.")
		return false
	}
	removeSave()
	fmt.Println()
	if i == s.CulpritIndex() {
		stop := spinner(s.BossName + "님이 보고서를 읽는 중")
		v, err := JudgeReasoning(ctx, llm, s, reason)
		stop()
		fmt.Printf("%s%s 정답! 범인은 %s %s였습니다.%s\n", cGreen, cBold, s.Culprit, s.Suspects[i].Title, cReset)
		if err == nil {
			printBubble(fmtClock(g.Minutes), cRed, s.BossName, v.Feedback)
			fmt.Printf("\n추리 등급: %s%s%s   남은 시간: %d분\n", cBold, v.Grade, cReset, endMin-g.Minutes)
		}
	} else {
		fmt.Printf("%s%s 오답... %s님은 억울하게 시말서를 쓰게 됐습니다.%s\n", cRed, cBold, s.Suspects[i].Name, cReset)
		printBubble(fmtClock(g.Minutes), cRed, s.BossName, "...이 사람 아니라는데요? 내일 아침에 저랑 따로 얘기 좀 합시다.")
	}
	reveal(s)
	exit(0)
	return true
}

func timeUp(g *Game) {
	s := g.Scenario
	removeSave()
	fmt.Println()
	printBubble("18:00", cRed, s.BossName, "6시네요. 결국 못 찾았군요... 대표님께는 제가 잘 말씀드려볼게요. 근데 이번 분기 평가는 기대하지 마세요.")
	fmt.Printf("\n%s%s시간 초과! 퇴근 시간이 지났습니다.%s\n", cRed, cBold, cReset)
	reveal(s)
	exit(0)
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
