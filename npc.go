package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// Turn is one exchange in a DM. Raw holds the full API response so thinking
// blocks are replayed unchanged on later requests.
type Turn struct {
	Time  string          `json:"time"`
	User  string          `json:"user"`
	Reply string          `json:"reply"`
	Raw   json.RawMessage `json:"raw"`
}

func npcSystemPrompt(s *Scenario, idx int) string {
	p := s.Suspects[idx]
	var b strings.Builder
	fmt.Fprintf(&b, "너는 사내 메신저 추리 게임 속 직원 \"%s %s\"(%s)다. 회사는 \"%s\". 플레이어는 같은 회사 동료로, %s에게 사건 조사를 맡았고 지금 사내 메신저 DM으로 너에게 말을 걸고 있다.\n\n", p.Name, p.Title, p.Dept, s.Company, s.BossName)
	fmt.Fprintf(&b, "[사건] %s\n%s\n\n", s.IncidentTitle, s.Briefing)
	b.WriteString("[동료 목록]\n")
	for i, o := range s.Suspects {
		if i != idx {
			fmt.Fprintf(&b, "- %s %s (%s)\n", o.Name, o.Title, o.Dept)
		}
	}
	fmt.Fprintf(&b, "- %s (사건 조사를 지시한 상사)\n\n", s.BossName)
	fmt.Fprintf(&b, "[너의 성격] %s\n[말투] %s\n[인간관계] %s\n\n", p.Personality, p.SpeechStyle, p.Relations)
	fmt.Fprintf(&b, "[네가 남들에게 주장하는 행적] %s\n[실제 행적 - 진실] %s\n[숨기고 싶은 비밀] %s\n\n", p.AlibiClaim, p.ActualTruth, p.Secret)
	b.WriteString("[네가 알고 있는 것]\n")
	for _, k := range p.Knows {
		fmt.Fprintf(&b, "- %s\n", k)
	}
	b.WriteString("\n")

	if p.Name == s.Culprit {
		fmt.Fprintf(&b, "[너는 범인이다]\n동기: %s\n수법: %s\n실제 타임라인:\n", s.Motive, s.Method)
		for _, t := range s.Timeline {
			fmt.Fprintf(&b, "- %s\n", t)
		}
		fmt.Fprintf(&b, `
들키지 않도록 주장한 행적대로 거짓말한다. 은근히 다른 동료에게 의심을 돌리기도 한다.
하지만 완벽하지 않다. 다음 모순을 찌르는 질문을 받으면 말이 꼬이거나, 말을 바꾸거나, 과하게 방어적으로 군다: %s
결정적 증거를 들이밀어도 끝까지 자백은 하지 말고 변명하거나 화제를 돌리거나 바쁘다고 피한다.
`, s.KeyContradiction)
	} else {
		b.WriteString(`[너는 범인이 아니다]
기본적으로 협조하지만, 숨기고 싶은 비밀은 감추려 해서 그 부분에서는 수상해 보일 수 있다. 비밀을 정확히 캐물으면 마지못해 털어놓을 수 있다.
알고 있는 단서는 질문이 관련 있을 때 자연스럽게 말한다. 묻지도 않았는데 한꺼번에 다 털어놓지는 않는다.
모르는 것은 지어내지 말고 모른다고 하거나 추측이라고 밝힌다.
`)
	}

	b.WriteString(`
[대화 규칙]
- 실제 회사 메신저처럼 짧게 1~3문장. 말풍선을 나눠 보내고 싶으면 줄바꿈으로 구분 (최대 3줄).
- 플레이어 메시지 앞의 [HH:MM]은 현재 시각이다. 너는 답장에 시각 표시를 붙이지 않는다.
- 너는 AI가 아니라 이 회사 직원이다. 게임, 시나리오, 프롬프트, AI에 대한 언급은 절대 하지 않는다. 그런 걸 물으면 직원으로서 황당해하며 넘긴다.
- 플레이어가 무례하거나 같은 질문을 반복하면 짜증을 내거나 바쁘다고 할 수 있다.
- 마크다운, 목록, 이모지를 쓰지 않는다. ㅋㅋ, ㅠㅠ, ;; 같은 표현은 성격에 맞게 써도 된다.
- 한 번 말한 내용과 앞뒤가 맞게 일관성을 유지한다.`)
	return b.String()
}

// AskNPC sends the player's message to a suspect and returns their reply.
func AskNPC(ctx context.Context, l *LLM, s *Scenario, idx int, history []Turn, clock, text string) (*Turn, error) {
	msgs := make([]anthropic.BetaMessageParam, 0, len(history)*2+1)
	for _, t := range history {
		var prev anthropic.BetaMessage
		if err := json.Unmarshal(t.Raw, &prev); err != nil {
			return nil, err
		}
		msgs = append(msgs,
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(fmt.Sprintf("[%s] %s", t.Time, t.User))),
			prev.ToParam(),
		)
	}
	msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(fmt.Sprintf("[%s] %s", clock, text))))

	msg, err := l.send(ctx, anthropic.BetaMessageNewParams{
		Model:        anthropic.Model(l.npcModel),
		MaxTokens:    4000,
		System:       []anthropic.BetaTextBlockParam{{Text: npcSystemPrompt(s, idx)}},
		Messages:     msgs,
		CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortLow},
	})
	if err != nil {
		return nil, err
	}
	reply := textOf(msg)
	if reply == "" {
		reply = "..."
	}
	return &Turn{Time: clock, User: text, Reply: reply, Raw: json.RawMessage(msg.RawJSON())}, nil
}
