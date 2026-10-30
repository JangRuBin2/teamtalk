package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

var (
	stdin = bufio.NewReader(os.Stdin)
	llm   *LLM
	ctx   = context.Background()
	cfg   Config
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

func yes(label string, def bool) bool {
	ans, ok := prompt(label)
	if !ok {
		return false
	}
	ans = strings.ToLower(ans)
	if ans == "" {
		return def
	}
	return strings.HasPrefix(ans, "y")
}

func waitEnter(label string) {
	fmt.Print(cDim + label + cReset)
	readLine()
}

func exit(code int) {
	if pauseOnExit {
		waitEnter("\nEnter를 누르면 창이 닫힙니다.")
	}
	os.Exit(code)
}

func main() {
	enableANSI()
	loadConfig()
	model := envOr("TEAMTALK_MODEL", defaultModel)
	llm = NewLLM(model, envOr("TEAMTALK_NPC_MODEL", model))

	for {
		clearScreen()
		fmt.Println(cBold + "TeamTalk for Work" + cReset + cDim + "  v3.3.0" + cReset)
		sysLine("%s", workStatus())
		fmt.Println()
		fmt.Printf("  1. 사내 추리      %s퇴근 전까지 동료들을 심문해 범인 찾기%s %s\n", cDim, cReset, whodunitStatus())
		fmt.Printf("  2. 설득왕         %sAI 상대를 정해진 턴 안에 말로 설득하기%s %s\n", cDim, cReset, persuadeStatus())
		fmt.Printf("  3. 메일함 생존기  %s업무 메일 10통에 답장하고 살아남기%s %s\n", cDim, cReset, mailStatus())
		fmt.Printf("  4. 서버 던전      %sssh로 접속하는 레거시 서버 텍스트 RPG%s %s\n", cDim, cReset, dungeonStatus())
		fmt.Println()
		fmt.Printf("  %s0. 설정 (출퇴근 시간, 접속)    q. 종료%s\n\n", cDim, cReset)
		choice, ok := prompt("> ")
		if !ok {
			exit(0)
		}
		switch strings.ToLower(choice) {
		case "1":
			playWhodunit()
		case "2":
			playPersuade()
		case "3":
			playMail()
		case "4":
			playDungeon()
		case "0":
			settings()
		case "q", "/q":
			exit(0)
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadConfig() {
	readJSON("config.json", &cfg)
	if cfg.APIKey == "" && relayEndpoint() == "" {
		if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
			cfg.APIKey = k
		} else {
			fmt.Println("처음 실행하셨네요. Anthropic API 키가 필요합니다. (https://console.anthropic.com)")
			cfg.APIKey = askAPIKey()
		}
	}
	if usingRelay() && cfg.AccessCode == "" {
		fmt.Println("처음 실행하셨네요. 받은 접속 코드를 입력해 주세요.")
		cfg.AccessCode = askAccessCode()
	}
	_, okStart := parseHM(cfg.WorkStart)
	_, okEnd := parseHM(cfg.WorkEnd)
	if !okStart || !okEnd {
		fmt.Println("출퇴근 시간을 등록해 주세요. 게임 시간이 실제 시각과 퇴근 시간에 맞춰 흘러갑니다.")
		askWorkHours()
	}
	saveConfig()
}

func saveConfig() {
	if err := writeJSON("config.json", cfg); err != nil {
		sysLine("설정 저장 실패(이번 실행에만 적용): %v", err)
	}
}

func askAPIKey() string {
	for {
		k, ok := prompt("API 키 입력: ")
		if !ok {
			exit(1)
		}
		if strings.HasPrefix(k, "sk-") {
			return k
		}
		fmt.Println("sk- 로 시작하는 키를 입력해 주세요.")
	}
}

func askAccessCode() string {
	for {
		c, ok := prompt("접속 코드: ")
		if !ok {
			exit(1)
		}
		if c != "" {
			return c
		}
	}
}

func askHM(label, def string) string {
	for {
		v, ok := prompt(fmt.Sprintf("%s [%s]: ", label, def))
		if !ok {
			exit(1)
		}
		if v == "" {
			return def
		}
		if len(v) == 4 && !strings.Contains(v, ":") { // 0930 -> 09:30
			v = v[:2] + ":" + v[2:]
		}
		if m, ok := parseHM(v); ok {
			return fmt.Sprintf("%02d:%02d", m/60, m%60)
		}
		fmt.Println("09:00 처럼 HH:MM 형식으로 입력해 주세요.")
	}
}

func askWorkHours() {
	for {
		start := askHM("출근 시간", or(cfg.WorkStart, "09:00"))
		end := askHM("퇴근 시간", or(cfg.WorkEnd, "18:00"))
		s, _ := parseHM(start)
		e, _ := parseHM(end)
		if e > s {
			cfg.WorkStart, cfg.WorkEnd = start, end
			return
		}
		fmt.Println("퇴근 시간은 출근 시간보다 늦어야 합니다.")
	}
}

func or(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func settings() {
	for {
		clearScreen()
		fmt.Println(cBold + "설정" + cReset)
		fmt.Printf("\n  1. 출퇴근 시간   %s ~ %s\n", cfg.WorkStart, cfg.WorkEnd)
		if usingRelay() {
			fmt.Printf("  2. 접속 코드     %s\n", maskKey(cfg.AccessCode))
			fmt.Printf("  3. 내 API 키로 바꾸기\n")
		} else {
			fmt.Printf("  2. API 키        %s\n", maskKey(cfg.APIKey))
			if relayEndpoint() != "" {
				fmt.Printf("  3. 접속 코드 방식으로 바꾸기 (내 API 키 지우기)\n")
			}
		}
		fmt.Printf("\n  %s0. 돌아가기%s\n\n", cDim, cReset)
		c, ok := prompt("> ")
		if !ok {
			return
		}
		switch c {
		case "1":
			askWorkHours()
			saveConfig()
			sysLine("저장했습니다. 이미 진행 중인 사내 추리의 마감 시간은 그대로입니다.")
			waitEnter("Enter")
		case "2":
			if usingRelay() {
				cfg.AccessCode = askAccessCode()
			} else {
				cfg.APIKey = askAPIKey()
			}
		case "3":
			if usingRelay() {
				cfg.APIKey = askAPIKey()
			} else if relayEndpoint() != "" {
				cfg.APIKey = ""
				if cfg.AccessCode == "" {
					cfg.AccessCode = askAccessCode()
				}
			}
		default:
			return
		}
		saveConfig()
		llm = NewLLM(llm.model, llm.npcModel)
	}
}

func maskKey(k string) string {
	if k == "" {
		return "(없음)"
	}
	if len(k) < 12 {
		return k[:min(3, len(k))] + "***"
	}
	return k[:7] + "..." + k[len(k)-4:]
}

// metaCommand handles commands shared by every game. It returns true if line was one.
func metaCommand(line string, redraw func()) bool {
	switch line {
	case "/b", "/boss":
		bossKey(readLine)
		redraw()
	case "/clear", "/cls":
		clearScreen()
		redraw()
	default:
		return false
	}
	return true
}
