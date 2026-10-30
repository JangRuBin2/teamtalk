package main

import (
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const (
	mailSave  = "mail.json"
	mailTotal = 10
)

type Email struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type MailResult struct {
	Reaction        string `json:"reaction"`
	ReputationDelta int    `json:"reputation_delta"`
	StressDelta     int    `json:"stress_delta"`
	NextEmail       Email  `json:"next_email"`
}

type MailGame struct {
	Reputation int    `json:"reputation"`
	Stress     int    `json:"stress"`
	Answered   int    `json:"answered"`
	Current    Email  `json:"current"`
	Received   string `json:"received"` // HH:MM the current mail arrived
	Chat       []Turn `json:"chat"`
}

const mailSystem = `너는 직장 생활 시뮬레이션 "메일함 생존기"의 게임 마스터다.
플레이어는 입사 3개월 차 마케팅팀 사원이다. 플레이어가 받은 메일에 답장을 쓰면 너는 그 답장을 평가하고 다음 메일을 보낸다.

등장인물(반복 등장시켜 이야기가 이어지게 한다):
- 최부장(마케팅팀장): 일을 떠넘기고 말을 자주 바꾼다
- 김대리(사수): 착하지만 자기 일도 은근히 넘긴다
- 디자인팀 윤과장: 일정에 예민하다
- 인사팀: 교육 이수, 연말정산 같은 귀찮은 공지
- 거래처 박실장: 무리한 요구를 정중하게 한다
- 그 외 필요하면 새로운 인물

평가 기준:
- 무성의하거나 너무 짧은 답장, 무례한 답장: 평판 하락
- 무리한 일을 다 떠안으면: 평판은 조금 오르지만 스트레스 크게 상승
- 정중하게 선을 긋거나 합리적 대안을 제시: 평판 유지 또는 상승, 스트레스 하락
- 센스 있고 웃긴 답장: 보너스
- reputation_delta, stress_delta는 각각 -20~20 정수

출력:
- reaction: 답장을 받은 사람의 반응이나 결과 1~2문장 (첫 턴에는 빈 문자열)
- next_email: 다음에 도착하는 메일. from은 "이름 직함 <영문id@company.co.kr>" 형식, body는 실제 업무 메일처럼 3~8줄. 이전 답장의 결과가 다음 메일에 반영되도록 한다(예: 일을 떠안았으면 그 일의 후폭풍).
- 플레이어 메시지 앞 [HH:MM]은 현재 시각이다. 시간대에 맞는 메일을 보낸다(아침 공지, 점심 직전 급한 요청, 퇴근 직전 폭탄 등).
- 현실적이고 웃픈 톤. 마크다운 금지.`

func mailStatus() string {
	var g MailGame
	if readJSON(mailSave, &g) != nil {
		return ""
	}
	return fmt.Sprintf("%s[진행 중 %d/%d]%s", cYellow, g.Answered, mailTotal, cReset)
}

func playMail() {
	g := &MailGame{}
	if readJSON(mailSave, g) != nil || !yes(fmt.Sprintf("진행 중인 메일함이 있습니다 (%d/%d). 이어서 할까요? [Y/n] ", g.Answered, mailTotal), true) {
		g = &MailGame{Reputation: 50, Stress: 30}
		stop := spinner("메일 동기화 중")
		turn, r, err := chatJSON[MailResult](llm.npcModel, mailSystem, nil, Turn{Time: fmtClock(now()), User: "출근했다. 첫 메일을 보내줘."}, anthropic.BetaOutputConfigEffortLow)
		stop()
		if err != nil {
			fmt.Println(cRed + "동기화 실패: " + friendlyError(err) + cReset)
			waitEnter("Enter")
			return
		}
		g.Chat = []Turn{*turn}
		g.Current, g.Received = r.NextEmail, fmtClock(now())
		writeJSON(mailSave, g)
	}
	clearScreen()
	drawMail(g)
	for {
		line, ok := prompt(fmt.Sprintf("\n%s답장%s (/q 저장 후 나가기) > ", cBold, cReset))
		if !ok || line == "/q" {
			return
		}
		if line == "" || metaCommand(line, func() { drawMail(g) }) {
			continue
		}
		stop := spinner("전송 중")
		turn, r, err := chatJSON[MailResult](llm.npcModel, mailSystem, g.Chat, Turn{Time: fmtClock(now()), User: "답장: " + line}, anthropic.BetaOutputConfigEffortLow)
		stop()
		if err != nil {
			sysLine("(전송 실패: %s)", friendlyError(err))
			continue
		}
		g.Chat = append(g.Chat, *turn)
		g.Answered++
		g.Reputation = max(0, min(100, g.Reputation+r.ReputationDelta))
		g.Stress = max(0, min(100, g.Stress+r.StressDelta))
		fmt.Printf("\n%s↳ %s%s\n", cDim, r.Reaction, cReset)
		fmt.Printf("%s  평판 %+d · 스트레스 %+d%s\n", cDim, r.ReputationDelta, r.StressDelta, cReset)
		if mailEnded(g) {
			removeFile(mailSave)
			waitEnter("\nEnter를 누르면 게임 메뉴로 돌아갑니다.")
			return
		}
		g.Current, g.Received = r.NextEmail, fmtClock(now())
		writeJSON(mailSave, g)
		waitEnter("Enter를 누르면 다음 메일")
		clearScreen()
		drawMail(g)
	}
}

func drawMail(g *MailGame) {
	fmt.Printf("%s── 받은편지함 (%d/%d) ── %s ──%s\n", cDim, g.Answered+1, mailTotal, workStatus(), cReset)
	fmt.Printf("평판 %s   스트레스 %s\n\n", bar(g.Reputation, cGreen), bar(g.Stress, cRed))
	e := g.Current
	fmt.Printf("%s보낸사람%s  %s\n", cDim, cReset, e.From)
	fmt.Printf("%s받은시각%s  %s\n", cDim, cReset, g.Received)
	fmt.Printf("%s제목%s      %s%s%s\n", cDim, cReset, cBold, e.Subject, cReset)
	fmt.Println(cDim + strings.Repeat("─", 50) + cReset)
	fmt.Println(strings.TrimSpace(e.Body))
	fmt.Println(cDim + strings.Repeat("─", 50) + cReset)
}

// mailEnded prints an ending and returns true when the day is over.
func mailEnded(g *MailGame) bool {
	var title, text string
	switch {
	case g.Stress >= 100:
		title, text = "번아웃 조퇴", "스트레스가 한계를 넘었습니다. 오늘은 반차 쓰고 집에 갑니다..."
	case g.Reputation <= 0:
		title, text = "인사팀 면담 소집", "평판이 바닥났습니다. 내일 10시 인사팀 면담이 잡혔습니다."
	case g.Answered < mailTotal:
		return false
	case g.Reputation >= 80:
		title, text = "이달의 사원", "모두가 당신의 답장 센스를 칭찬합니다. 최부장도 처음으로 웃었습니다."
	case g.Reputation >= 50 && g.Stress < 70:
		title, text = "무난한 하루", "큰 사고 없이 메일함을 비웠습니다. 칼퇴 각입니다."
	default:
		title, text = "내일이 두렵다", "어떻게든 버텼지만, 내일 아침 메일함을 열기가 무섭습니다."
	}
	fmt.Printf("\n%s── 엔딩: %s ──%s\n%s\n", cBold, title, cReset, text)
	fmt.Printf("최종 평판 %d · 스트레스 %d · 답장 %d통\n", g.Reputation, g.Stress, g.Answered)
	return true
}
