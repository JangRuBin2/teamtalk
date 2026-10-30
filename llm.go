package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/anthropics/anthropic-sdk-go/v2/shared/constant"
)

const (
	defaultModel    = "claude-opus-5-5"  // case generation, where quality matters most
	defaultNPCModel = "claude-haiku-5-5" // in-game chat and grading
)

// relayURL is the built-in relay server, set at build time with
// -ldflags "-X main.relayURL=https://...". The relay holds the API key, so
// players only need an access code.
var relayURL = ""

func relayEndpoint() string { return or(cfg.RelayURL, relayURL) }

// usingRelay reports whether requests go through the relay instead of a personal API key.
func usingRelay() bool { return cfg.APIKey == "" && relayEndpoint() != "" }

// errRefused is returned when the model declines to answer (stop_reason "refusal").
var errRefused = errors.New("refused")

type LLM struct {
	client   anthropic.Client
	model    string // scenario generation
	npcModel string // NPC chat, game masters, grading
}

func NewLLM(model, npcModel string) *LLM {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if usingRelay() {
		opts = []option.RequestOption{
			option.WithBaseURL(relayEndpoint()),
			option.WithAPIKey("relay"), // replaced by the relay
			option.WithHeader("x-teamtalk-token", cfg.AccessCode),
		}
	}
	return &LLM{client: anthropic.NewClient(opts...), model: model, npcModel: npcModel}
}

// supportsDefaultFallback reports whether the server-side "default" refusal
// fallback can be sent for this model.
func supportsDefaultFallback(model string) bool {
	return strings.HasPrefix(model, "claude-opus-5") ||
		strings.HasPrefix(model, "claude-fable-5") ||
		model == "claude-sonnet-5-5"
}

func (l *LLM) send(ctx context.Context, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
	if supportsDefaultFallback(string(p.Model)) {
		p.Betas = append(p.Betas, anthropic.AnthropicBetaServerSideFallback2026_07_01)
		p.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	msg, err := l.client.Beta.Messages.New(ctx, p)
	if err != nil {
		return nil, err
	}
	if msg.StopReason == anthropic.BetaStopReasonRefusal {
		return nil, errRefused
	}
	return msg, nil
}

func textOf(m *anthropic.BetaMessage) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if t, ok := b.AsAny().(anthropic.BetaTextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

// friendlyError turns API errors into a short Korean message for the player.
func friendlyError(err error) string {
	if errors.Is(err, errRefused) {
		return "상대방이 답장을 거부했습니다. 다른 질문을 해보세요."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "응답 시간이 초과됐습니다. 다시 시도해 주세요."
	}
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401:
			if usingRelay() {
				return "접속 코드가 올바르지 않습니다. 메뉴의 설정(0)에서 다시 입력하세요."
			}
			return "API 키가 올바르지 않습니다. 메뉴의 설정(0)에서 다시 입력하세요."
		case 403:
			return "이 모델은 사용할 수 없습니다."
		case 429:
			if usingRelay() {
				return "오늘 사용량을 다 썼거나 요청이 너무 많습니다. 내일 다시 해주세요."
			}
			return "요청이 너무 많습니다. 잠시 후 다시 시도해 주세요."
		case 529, 500, 502, 503:
			return "서버가 혼잡합니다. 잠시 후 다시 시도해 주세요."
		default:
			return fmt.Sprintf("API 오류 (%d)", apierr.StatusCode)
		}
	}
	return "네트워크 오류: " + err.Error()
}
