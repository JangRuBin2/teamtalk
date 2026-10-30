package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// Case drafts are Markdown files that fix every fact of a mystery (culprit,
// timeline, alibis, clues, evidence). At start-up the names are filled in
// locally and the AI only writes personalities, speech styles, relations and
// the boss's briefing, which keeps generation short.
//
// Placeholders: {A}..{F} for suspects, {회사}, {팀장}. See cases/README.md.

//go:embed cases/*.md
var bundledCases embed.FS

type caseDraft struct {
	ID       string
	Title    string
	Overview string
	Culprit  string // suspect letter
	Motive   string
	Method   string
	Key      string
	Timeline []string
	Suspects []draftSuspect
	Evidence []Evidence
}

type draftSuspect struct {
	Letter, Title, Dept, Hint, Claim, Truth, Secret string
	Knows                                           []string
}

func parseCase(id, text string) (*caseDraft, error) {
	d := &caseDraft{ID: id}
	var section string
	var sus *draftSuspect
	var evi *Evidence
	inClues := false
	para := map[string]*string{"개요": &d.Overview, "범인": &d.Culprit, "동기": &d.Motive, "수법": &d.Method, "결정적 모순": &d.Key}

	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "# "):
			d.Title = strings.TrimSpace(line[2:])
			continue
		case strings.HasPrefix(line, "## "):
			section, sus, evi, inClues = strings.TrimSpace(line[3:]), nil, nil, false
			continue
		case strings.HasPrefix(line, "### "):
			name := strings.TrimSpace(line[4:])
			switch section {
			case "용의자":
				d.Suspects = append(d.Suspects, draftSuspect{Letter: name})
				sus, inClues = &d.Suspects[len(d.Suspects)-1], false
			case "증거":
				d.Evidence = append(d.Evidence, Evidence{Title: name})
				evi = &d.Evidence[len(d.Evidence)-1]
			}
			continue
		}
		switch {
		case section == "증거" && evi != nil:
			if !strings.HasPrefix(trim, "```") {
				evi.Content += line + "\n"
			}
		case section == "타임라인" && strings.HasPrefix(trim, "- "):
			d.Timeline = append(d.Timeline, strings.TrimSpace(trim[2:]))
		case section == "용의자" && sus != nil && trim != "":
			indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
			if inClues && indented && strings.HasPrefix(trim, "- ") {
				sus.Knows = append(sus.Knows, strings.TrimSpace(trim[2:]))
				continue
			}
			key, val, ok := strings.Cut(strings.TrimPrefix(trim, "- "), ":")
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			inClues = false
			switch strings.TrimSpace(key) {
			case "직급":
				sus.Title = val
			case "부서":
				sus.Dept = val
			case "성격 힌트":
				sus.Hint = val
			case "주장":
				sus.Claim = val
			case "실제":
				sus.Truth = val
			case "비밀":
				sus.Secret = val
			case "단서":
				inClues = true
			}
		case para[section] != nil && trim != "":
			p := para[section]
			if *p != "" {
				*p += " "
			}
			*p += trim
		}
	}
	for i := range d.Evidence {
		d.Evidence[i].Content = strings.Trim(d.Evidence[i].Content, "\n")
	}
	d.Culprit = strings.Trim(d.Culprit, "{} ")

	switch {
	case d.Title == "":
		return nil, fmt.Errorf("%s: 제목(# ...)이 없습니다", id)
	case len(d.Suspects) < 4 || len(d.Suspects) > 6:
		return nil, fmt.Errorf("%s: 용의자는 4~6명이어야 합니다 (%d명)", id, len(d.Suspects))
	case len(d.Evidence) < 3:
		return nil, fmt.Errorf("%s: 증거가 3개 이상 필요합니다", id)
	case d.Key == "" || len(d.Timeline) == 0:
		return nil, fmt.Errorf("%s: 결정적 모순과 타임라인이 필요합니다", id)
	}
	found := false
	for _, s := range d.Suspects {
		if s.Letter == d.Culprit {
			found = true
		}
		if s.Claim == "" || s.Truth == "" {
			return nil, fmt.Errorf("%s: 용의자 %s의 주장/실제가 비어 있습니다", id, s.Letter)
		}
	}
	if !found {
		return nil, fmt.Errorf("%s: 범인 %q가 용의자 목록에 없습니다", id, d.Culprit)
	}
	return d, nil
}

// loadCases reads the bundled drafts plus any the player put in ~/.teamtalk/cases.
func loadCases() ([]*caseDraft, []error) {
	var out []*caseDraft
	var errs []error
	add := func(id string, b []byte, err error) {
		if err != nil {
			errs = append(errs, err)
			return
		}
		d, err := parseCase(id, string(b))
		if err != nil {
			errs = append(errs, err)
			return
		}
		out = append(out, d)
	}
	names, _ := fs.Glob(bundledCases, "cases/*.md")
	for _, n := range names {
		if strings.HasSuffix(n, "README.md") {
			continue
		}
		b, err := bundledCases.ReadFile(n)
		add(strings.TrimSuffix(filepath.Base(n), ".md"), b, err)
	}
	user, _ := filepath.Glob(filepath.Join(dataDir(), "cases", "*.md"))
	for _, n := range user {
		b, err := os.ReadFile(n)
		add("user:"+strings.TrimSuffix(filepath.Base(n), ".md"), b, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, errs
}

const playedFile = "cases_played.json"

// pickCase returns a random draft the player hasn't played yet, starting
// over once every draft has been played.
func pickCase() (*caseDraft, error) {
	all, errs := loadCases()
	if len(all) == 0 {
		if len(errs) > 0 {
			return nil, errs[0]
		}
		return nil, fmt.Errorf("사건 파일이 없습니다")
	}
	var played []string
	readJSON(playedFile, &played)
	seen := map[string]bool{}
	for _, id := range played {
		seen[id] = true
	}
	var fresh []*caseDraft
	for _, d := range all {
		if !seen[d.ID] {
			fresh = append(fresh, d)
		}
	}
	if len(fresh) == 0 { // everything played: start over
		writeJSON(playedFile, []string{})
		fresh = all
	}
	return fresh[rand.Intn(len(fresh))], nil
}

func markPlayed(id string) {
	var played []string
	readJSON(playedFile, &played)
	for _, p := range played {
		if p == id {
			return
		}
	}
	writeJSON(playedFile, append(played, id))
}

var (
	surnames   = []string{"김", "이", "박", "최", "정", "강", "조", "윤", "장", "임", "한", "오", "서", "신", "권", "황", "안", "송", "류", "홍", "전", "고", "문", "양"}
	givenNames = []string{"민준", "서연", "도윤", "지우", "하은", "시우", "지민", "예준", "수아", "현우", "지아", "준서", "다은", "건우", "채원", "우진", "소율", "선우", "유진", "민재", "지호", "서진", "하린", "태윤", "나연", "승현", "가은", "재원", "보영", "동현", "은지", "상훈", "미래", "정우", "혜린", "성민"}
	bossGiven  = []string{"정훈", "성호", "미경", "재석", "은숙", "영진", "태식", "경희", "동수", "순영"}
	companies  = []string{"넥스트웨이브", "블루오션랩", "한빛솔루션", "주식회사 다온", "스텔라소프트", "모아커머스", "그린브릿지", "주식회사 새봄", "오르빗시스템즈", "라이트하우스", "코어플랜", "주식회사 누리", "팔레트랩스", "하이브릿지", "온새미로"}
)

// castNames assigns distinct names to the suspects and the boss.
func castNames(n int) (people []string, boss string) {
	sur := rand.Perm(len(surnames))
	given := rand.Perm(len(givenNames))
	for i := 0; i < n; i++ {
		people = append(people, surnames[sur[i]]+givenNames[given[i]])
	}
	boss = surnames[sur[n]] + bossGiven[rand.Intn(len(bossGiven))] + " 팀장"
	return people, boss
}

type castFill struct {
	Briefing string        `json:"briefing"`
	Suspects []castSuspect `json:"suspects"`
}

type castSuspect struct {
	Personality string `json:"personality"`
	SpeechStyle string `json:"speech_style"`
	Relations   string `json:"relations"`
}

const castPrompt = `다음은 사내 메신저 추리 게임의 사건 초안이다. 사건의 사실(범인, 동기, 수법, 타임라인, 각 인물의 주장과 실제 행적, 비밀, 단서, 증거)은 이미 확정됐고 절대 바꾸지 않는다.
너는 등장인물에 생기를 불어넣는 일만 한다.

[초안]
%s

작성할 것:
- suspects: 위 용의자 순서 그대로, 사람마다
  - personality: 성격 2~3문장. "성격 힌트"가 있으면 따른다. 숨긴 비밀과 자연스럽게 어울리게. 5명이 서로 확실히 구별되게.
  - speech_style: 메신저 말투 설명 + 실제 말투 예시 문장 2개.
  - relations: 다른 용의자들과의 관계 1~2문장 (이름을 써서).
- briefing: %s가 플레이어에게 #general 채널로 보내는 메시지 2~4문장. 사건 개요와 "%s까지 범인 찾아와"라는 취지. 범인에 대한 힌트는 주지 않는다.
- 너무 무겁지 않게, 직장인이 웃을 수 있는 현실 공감 톤.`

// BuildFromDraft picks a case draft, casts it, and asks the AI only for the
// characterization.
func BuildFromDraft(ctx context.Context, l *LLM, deadline string) (*Scenario, error) {
	d, err := pickCase()
	if err != nil {
		return nil, err
	}
	names, boss := castNames(len(d.Suspects))
	company := companies[rand.Intn(len(companies))]
	pairs := []string{"{회사}", company, "{팀장}", boss}
	for i, s := range d.Suspects {
		pairs = append(pairs, "{"+s.Letter+"}", names[i])
	}
	r := strings.NewReplacer(pairs...).Replace

	s := &Scenario{
		Company:          company,
		BossName:         boss,
		IncidentTitle:    r(d.Title),
		Motive:           r(d.Motive),
		Method:           r(d.Method),
		KeyContradiction: r(d.Key),
	}
	for _, t := range d.Timeline {
		s.Timeline = append(s.Timeline, r(t))
	}
	for _, e := range d.Evidence {
		s.Evidence = append(s.Evidence, Evidence{Title: r(e.Title), Content: r(e.Content)})
	}
	var sheet strings.Builder
	fmt.Fprintf(&sheet, "회사: %s\n사건: %s\n개요: %s\n\n", company, s.IncidentTitle, r(d.Overview))
	for i, x := range d.Suspects {
		p := Suspect{Name: names[i], Title: x.Title, Dept: x.Dept, AlibiClaim: r(x.Claim), ActualTruth: r(x.Truth), Secret: r(x.Secret)}
		for _, k := range x.Knows {
			p.Knows = append(p.Knows, r(k))
		}
		if x.Letter == d.Culprit {
			s.Culprit = p.Name
		}
		s.Suspects = append(s.Suspects, p)
		fmt.Fprintf(&sheet, "%d. %s %s (%s)\n   성격 힌트: %s\n   비밀: %s\n   범인 여부: %v\n", i+1, p.Name, p.Title, p.Dept, or(r(x.Hint), "없음"), p.Secret, x.Letter == d.Culprit)
	}

	msg, err := l.send(ctx, anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(l.model),
		MaxTokens: 8000,
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(fmt.Sprintf(castPrompt, sheet.String(), boss, deadline))),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: &castFill{}},
		},
	})
	if err != nil {
		return nil, err
	}
	var fill castFill
	if err := json.Unmarshal([]byte(textOf(msg)), &fill); err != nil || len(fill.Suspects) != len(s.Suspects) {
		return nil, fmt.Errorf("인물 설정 생성 실패")
	}
	markPlayed(d.ID)
	s.Briefing = fill.Briefing
	for i, f := range fill.Suspects {
		s.Suspects[i].Personality, s.Suspects[i].SpeechStyle, s.Suspects[i].Relations = f.Personality, f.SpeechStyle, f.Relations
	}
	return s, s.validate()
}
