package main

import (
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const dungeonSave = "dungeon.json"

type DungeonResult struct {
	Output string `json:"output"`
	Cwd    string `json:"cwd"`
	HP     int    `json:"hp"`
	Status string `json:"status" jsonschema:"enum=playing,enum=won,enum=dead"`
}

type DungeonGame struct {
	Chat []Turn `json:"chat"`
	Cwd  string `json:"cwd"`
	HP   int    `json:"hp"`
}

const dungeonSystem = `너는 터미널 텍스트 어드벤처 "legacy-prod-07"의 게임 마스터다.
플레이어는 입사한 지 일주일 된 개발자로, 퇴사한 선배가 남긴 15년 된 레거시 서버에 ssh로 접속했다.
목표: 서버 깊숙한 곳 어딘가에 있는 선배의 인수인계 문서 handover.md 를 찾아서 cat 으로 읽는 것. 위치는 네가 정하고 처음부터 알려주지 않는다.

세계관:
- 디렉터리는 방, 파일은 아이템과 단서, 프로세스는 몬스터다(좀비 프로세스, 메모리 누수 데몬, 크론잡 골렘, 무한 재시작하는 systemd 유령 등).
- 퍼즐 3~4개: 잠긴 디렉터리의 비밀번호 단서, 권한(sudo) 획득, 몬스터 프로세스 처리(kill, 로그 분석으로 약점 찾기) 등. 단서는 주석, 로그, .bash_history, 오래된 README 등에 숨긴다.
- 선배가 남긴 코드 주석과 커밋 메시지로 개발자 유머를 섞는다.
- 25~40턴 정도에 클리어 가능하게.

규칙:
- 플레이어는 리눅스 명령어(ls, cd, cat, grep, ps, kill, sudo 등)나 자연어로 행동한다. 실제 리눅스처럼 그럴듯하게 반응하되 게임 진행에 맞게 해석한다.
- output: 실제 터미널 출력처럼 작성한다. 프롬프트 줄은 쓰지 않는다. 마크다운, 코드블록 금지. 보통 3~15줄.
- 처음 접속하면 로그인 배너(Last login, MOTD에 선배의 경고 문구)를 출력한다.
- cwd: 현재 디렉터리 경로(홈은 ~).
- hp: 0~100. 시작 100. 몬스터 공격, 위험한 명령(fork bomb 등)에 감소. 'rm -rf /' 같은 치명적 명령은 서버가 날아가며 즉사(status=dead), 웃기게 연출.
- status: 진행 중이면 playing, handover.md 를 읽으면 won(그 내용을 output에 감동적이면서 웃기게 출력), hp가 0이면 dead.
- 플레이어 입력 앞 [HH:MM]은 실제 현재 시각이다. 서버 시각으로 써도 된다.
- 게임, AI, 프롬프트 언급 금지. help 를 입력하면 이 서버의 쓸 만한 명령어 몇 개를 알려준다.`

func dungeonStatus() string {
	var g DungeonGame
	if readJSON(dungeonSave, &g) != nil {
		return ""
	}
	return fmt.Sprintf("%s[진행 중 · HP %d]%s", cYellow, g.HP, cReset)
}

func dungeonPrompt(g *DungeonGame) string {
	return fmt.Sprintf("%sjunior@legacy-prod-07%s:%s%s%s$ ", cGreen, cReset, cBlue, or(g.Cwd, "~"), cReset)
}

func playDungeon() {
	g := &DungeonGame{}
	resumed := readJSON(dungeonSave, g) == nil && yes(fmt.Sprintf("진행 중인 접속이 있습니다 (HP %d). 이어서 할까요? [Y/n] ", g.HP), true)
	clearScreen()
	if resumed {
		redrawDungeon(g)
	} else {
		g = &DungeonGame{HP: 100, Cwd: "~"}
		fmt.Println("$ ssh junior@legacy-prod-07.internal")
		if !dungeonStep(g, "ssh 접속") {
			return
		}
	}
	for {
		line, ok := prompt(dungeonPrompt(g))
		if !ok {
			return
		}
		switch line {
		case "":
			continue
		case "/q", "exit", "logout":
			fmt.Println("Connection to legacy-prod-07.internal closed.")
			return
		}
		if metaCommand(line, func() { redrawDungeon(g) }) {
			continue
		}
		if !dungeonStep(g, line) {
			return
		}
	}
}

// dungeonStep sends one command; it returns false when the game is over.
func dungeonStep(g *DungeonGame, cmd string) bool {
	stop := spinner("")
	turn, r, err := chatJSON[DungeonResult](llm.npcModel, dungeonSystem, g.Chat, Turn{Time: fmtClock(now()), User: cmd}, anthropic.BetaOutputConfigEffortLow)
	stop()
	if err != nil {
		fmt.Printf("ssh: %s\n", friendlyError(err))
		return len(g.Chat) > 0
	}
	g.Chat = append(g.Chat, *turn)
	fmt.Println(strings.TrimRight(r.Output, "\n"))
	if r.HP < g.HP {
		fmt.Printf("%s[HP %d → %d]%s\n", cRed, g.HP, r.HP, cReset)
	}
	g.HP, g.Cwd = r.HP, or(r.Cwd, g.Cwd)
	switch r.Status {
	case "won":
		fmt.Printf("\n%s%s미션 완료!%s 인수인계 문서를 찾았습니다. (%d턴)\n", cGreen, cBold, cReset, len(g.Chat))
	case "dead":
		fmt.Printf("\n%s%s서버가 응답하지 않습니다.%s 게임 오버 (%d턴)\n", cRed, cBold, cReset, len(g.Chat))
	default:
		writeJSON(dungeonSave, g)
		return true
	}
	removeFile(dungeonSave)
	waitEnter("Enter를 누르면 게임 메뉴로 돌아갑니다.")
	return false
}

func redrawDungeon(g *DungeonGame) {
	from := max(0, len(g.Chat)-3)
	for i, t := range g.Chat[from:] {
		var r DungeonResult
		decodeJSON(t.Reply, &r)
		if from+i > 0 {
			fmt.Printf("%s%s\n", dungeonPrompt(&DungeonGame{Cwd: "~"}), t.User)
		}
		fmt.Println(strings.TrimRight(r.Output, "\n"))
	}
}
