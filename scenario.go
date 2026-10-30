package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

type Suspect struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Dept        string   `json:"dept"`
	Personality string   `json:"personality"`
	SpeechStyle string   `json:"speech_style"`
	AlibiClaim  string   `json:"alibi_claim"`
	ActualTruth string   `json:"actual_whereabouts"`
	Secret      string   `json:"secret"`
	Knows       []string `json:"knows"`
	Relations   string   `json:"relations"`
}

type Evidence struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type Scenario struct {
	Company          string     `json:"company"`
	BossName         string     `json:"boss_name"`
	IncidentTitle    string     `json:"incident_title"`
	Briefing         string     `json:"briefing"`
	Suspects         []Suspect  `json:"suspects"`
	Culprit          string     `json:"culprit"`
	Motive           string     `json:"motive"`
	Method           string     `json:"method"`
	Timeline         []string   `json:"timeline"`
	Evidence         []Evidence `json:"evidence"`
	KeyContradiction string     `json:"key_contradiction"`
}

func (s *Scenario) CulpritIndex() int {
	for i, p := range s.Suspects {
		if p.Name == s.Culprit {
			return i
		}
	}
	return -1
}

func (s *Scenario) validate() error {
	if len(s.Suspects) < 4 || len(s.Suspects) > 6 {
		return fmt.Errorf("용의자 수가 이상함: %d", len(s.Suspects))
	}
	if s.CulpritIndex() < 0 {
		return fmt.Errorf("범인 %q 이(가) 용의자 목록에 없음", s.Culprit)
	}
	if len(s.Evidence) < 3 {
		return fmt.Errorf("증거가 너무 적음: %d", len(s.Evidence))
	}
	return nil
}

var themes = []string{
	"탕비실에 있던 대표님의 고급 원두 커피가 통째로 사라진 사건",
	"대표님이 아끼던 사무실 화분이 누군가에 의해 말라 죽은 사건",
	"분기 매출 보고서 원본 파일이 공유 드라이브에서 삭제된 사건",
	"법인카드로 새벽 2시에 노래방 결제가 찍힌 사건",
	"신제품 기획서가 경쟁사에 유출된 정황이 발견된 사건",
	"회식비 정산 금액이 실제보다 30만 원 부풀려진 사건",
	"팀장 모니터에 익명의 협박 포스트잇이 붙어 있던 사건",
	"운영 서버가 금요일 밤에 누군가의 명령어로 다운된 사건",
	"사내 냉장고의 남의 도시락만 골라서 사라지는 연쇄 사건",
	"사내 익명 게시판에 팀 내부 사정을 폭로하는 글이 올라온 사건",
	"창립기념일 행사 경품(최신 태블릿)이 보관함에서 사라진 사건",
	"인사평가 결과 파일이 전사 메일로 잘못 발송된 사건",
}

const scenarioPrompt = `사내 메신저 기반 추리 게임의 사건 시나리오를 만들어라. 플레이어는 이 회사 직원이며, 오늘 {{START}}에 팀장에게 사건 조사를 맡았다. {{DEADLINE}}까지 동료들에게 메신저로 질문하고 증거를 열람해서 범인을 찾아야 한다.

사건 주제: {{THEME}}

요구사항:
- 한국 회사 배경. 회사 이름(company)과 팀장 이름(boss_name, 예: "박정훈 팀장")을 정한다.
- 용의자(suspects)는 정확히 5명. 성이 서로 다른 한국 이름(name은 이름만, 예: "김민준"), 직급(title, 예: 대리/과장/사원/차장/주임), 부서(dept)가 다양해야 한다.
- 각 용의자의 personality와 speech_style은 서로 확실히 구별되게 (예: 눈치 보는 막내, 말 많은 오지랖, 퉁명스러운 개발자, 정치적인 과장 등). speech_style에는 실제 말투 예시 문장을 포함.
- alibi_claim: 사건 시간대에 무엇을 했다고 주장하는지. actual_whereabouts: 실제로 한 일(진실).
- 범인이 아닌 사람들도 각자 사건과 무관한 숨기고 싶은 비밀(secret)이 있어서 수상해 보여야 한다(레드 헤링). 범인에게도 secret을 준다.
- knows: 각자 알고 있는 단서 2~4개. 다른 동료에 대해 목격한 것, 들은 것 등. 무고한 사람들의 단서를 조합하면 범인을 좁힐 수 있어야 한다. 범인을 가리키는 단서는 최소 두 명 이상의 다른 사람에게 흩어져 있어야 한다.
- relations: 다른 동료들과의 관계(사이 좋음/나쁨, 라이벌 등).
- culprit: 범인의 이름(suspects의 name 중 하나와 정확히 일치). motive, method는 구체적으로.
- timeline: 사건 전후 실제로 일어난 일을 시간순으로 6~10개 ("어제 19:40 - ..." 형식).
- evidence: 플레이어가 열람할 수 있는 자료 4~5개. 출입 기록, 메일, CCTV 요약, 결제 내역, 서버 로그, 메신저 캡처 등. content는 실제 로그/문서처럼 여러 줄 텍스트로 시각과 함께 작성. 범인 이름을 직접 지목하면 안 되고, 증언과 교차 확인해야 의미가 드러나게. 레드 헤링 1개 포함.
- key_contradiction: 범인의 거짓말이 들통나는 결정적 모순 (증거 X와 범인의 주장 Y가 충돌하는 지점). 공정하게 추리 가능해야 한다.
- briefing: 팀장이 #general 채널에서 플레이어에게 보내는 메시지. 2~4문장, 사건 개요 + "{{DEADLINE}}까지 범인 찾아와" 라는 취지.
- 너무 무겁지 않게, 직장인이 웃을 수 있는 현실 공감 톤.`

// GenerateScenario builds a new case. start and deadline describe the real
// play window, e.g. "10:42" and "오늘 18:00 퇴근 전".
func GenerateScenario(ctx context.Context, l *LLM, start, deadline string) (*Scenario, error) {
	theme := themes[rand.Intn(len(themes))]
	userPrompt := strings.NewReplacer("{{START}}", start, "{{DEADLINE}}", deadline, "{{THEME}}", theme).Replace(scenarioPrompt)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		msg, err := l.send(ctx, anthropic.BetaMessageNewParams{
			Model:     anthropic.Model(l.model),
			MaxTokens: 16000,
			Messages: []anthropic.BetaMessageParam{
				anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(userPrompt)),
			},
			OutputConfig: anthropic.BetaOutputConfigParam{
				Effort: anthropic.BetaOutputConfigEffortMedium,
				Format: anthropic.BetaJSONOutputFormatParam{Schema: &Scenario{}},
			},
		})
		if err != nil {
			return nil, err
		}
		var s Scenario
		if err := json.Unmarshal([]byte(textOf(msg)), &s); err != nil {
			lastErr = fmt.Errorf("시나리오 파싱 실패: %w", err)
			continue
		}
		s.Culprit = strings.TrimSpace(s.Culprit)
		if err := s.validate(); err != nil {
			lastErr = err
			continue
		}
		return &s, nil
	}
	return nil, lastErr
}

type Verdict struct {
	Grade    string `json:"grade" jsonschema:"enum=S,enum=A,enum=B,enum=C"`
	Feedback string `json:"feedback"`
}

// JudgeReasoning grades the player's reasoning for a correct accusation.
func JudgeReasoning(ctx context.Context, l *LLM, s *Scenario, reasoning string) (*Verdict, error) {
	truth, _ := json.Marshal(struct {
		Culprit, Motive, Method, KeyContradiction string
		Timeline                                  []string
	}{s.Culprit, s.Motive, s.Method, s.KeyContradiction, s.Timeline})
	prompt := fmt.Sprintf(`추리 게임에서 플레이어가 범인을 맞혔다. 플레이어가 제시한 근거가 사건의 진실과 얼마나 맞는지 평가해라.

[사건의 진실]
%s

[플레이어의 근거]
%s

grade 기준: S = 결정적 모순(key_contradiction)이나 수법을 정확히 짚음, A = 동기나 핵심 증거 일부를 맞힘, B = 막연하지만 방향은 맞음, C = 사실상 찍음.
feedback: %s가 플레이어에게 메신저로 보내는 말투로 1~2문장. 등급에 맞게 칭찬하거나 비꼬기.`, truth, reasoning, s.BossName)
	msg, err := l.send(ctx, anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(l.model),
		MaxTokens: 4000,
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(prompt)),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: &Verdict{}},
		},
	})
	if err != nil {
		return nil, err
	}
	var v Verdict
	if err := json.Unmarshal([]byte(textOf(msg)), &v); err != nil {
		return nil, err
	}
	return &v, nil
}
