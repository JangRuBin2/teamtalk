# TeamTalk 중계 서버 설치 가이드

중계 서버는 두 가지 일을 합니다.

1. **AI 호출 대신 해주기:** 게임은 Anthropic API를 직접 부르지 않고 이 서버를 거칩니다. API 키는 이 서버에만 저장되고, 게임 파일에는 들어가지 않습니다. 플레이어는 내가 나눠준 **접속 코드**만 있으면 됩니다.
2. **같이하기 방:** 방장이 방을 만들면 6자리 **초대 코드**가 나오고, 동료는 그 코드만 입력하면 들어옵니다. IP를 알 필요도 없고, 사내망이 아니어도 됩니다.

Cloudflare Workers 무료 플랜으로 충분합니다. 하루 요청 10만 건까지 무료이고, 같이하기 방(Durable Objects)도 무료 플랜에서 됩니다.

---

## 0. 준비물

- **Cloudflare 계정:** https://dash.cloudflare.com/sign-up 에서 이메일로 가입합니다. 카드 등록은 필요 없습니다.
- **Node.js 18 이상:** https://nodejs.org 에서 LTS 버전을 설치합니다. 터미널에서 `node -v`를 쳐서 버전이 나오면 됩니다.
- **Anthropic API 키:** https://console.anthropic.com → API Keys → Create Key. 이 서버 전용으로 새로 만드세요(아래 6번 참고).
- **이 레포를 내 PC에 받기:**

```bash
git clone https://github.com/JangRuBin2/teamtalk.git
```

```bash
cd teamtalk/relay
```

이후 명령은 모두 이 `relay` 폴더 안에서 실행합니다.

---

## 1. Cloudflare 로그인

```bash
npx wrangler login
```

wrangler를 설치할지 물으면 `y`를 누르세요. 브라우저가 열리면 Cloudflare에 로그인하고 **Allow**를 누릅니다. 터미널에 `Successfully logged in`이 나오면 됩니다.

## 2. API 키 저장

```bash
npx wrangler secret put ANTHROPIC_API_KEY
```

`Enter a secret value:`가 나오면 `sk-ant-...`로 시작하는 키를 붙여넣고 Enter를 누릅니다. 입력한 글자는 화면에 안 보이는 게 정상입니다.

처음 실행하면 "Worker가 아직 없는데 만들까요?"라고 물을 수 있습니다. `y`를 누르면 됩니다.

## 3. 접속 코드 정하기

접속 코드는 **AI를 쓸 수 있는 사람**에게 주는 비밀번호입니다. 혼자 하는 게임을 할 때, 그리고 같이하기에서 **방을 만들 때** 필요합니다. 방에 참가만 하는 사람은 필요 없습니다.

사람마다 다른 코드를 만드세요. 그래야 나중에 한 명만 끊을 수 있습니다. 남이 추측하기 어렵게 이름 뒤에 아무 글자를 붙이면 됩니다.

예: `me-x8k2p9`, `minsu-7f3aq1`, `jiyoung-91bcz4`

```bash
npx wrangler secret put ACCESS_TOKENS
```

값에는 쉼표로 이어서 넣습니다. 예: `me-x8k2p9,minsu-7f3aq1,jiyoung-91bcz4`

코드를 추가하거나 빼고 싶으면 같은 명령을 다시 실행해서 **전체 목록**을 새로 넣으면 됩니다.

## 4. (선택) 하루 사용량 제한 켜기

접속 코드마다 하루에 몇 번까지 AI를 부를 수 있는지 제한합니다. 기본값은 500번입니다. 같이하기에서는 방장의 접속 코드로 팀 전체의 대화가 나가니 넉넉하게 잡았습니다.

```bash
npx wrangler kv namespace create USAGE
```

출력에 `id = "abc123..."` 같은 줄이 나옵니다. `wrangler.toml` 파일을 열어 맨 아래 주석 부분을 이렇게 바꿉니다(`#`을 지우고 id를 붙여넣기).

```toml
[[kv_namespaces]]
binding = "USAGE"
id = "abc123..."
```

제한 횟수를 바꾸려면 `wrangler.toml`의 `DAILY_LIMIT` 값을 고치세요.

## 5. 배포

```bash
npx wrangler deploy
```

끝나면 이런 주소가 나옵니다. **이 주소를 복사해 두세요.**

```
https://teamtalk-relay.<내계정이름>.workers.dev
```

잘 됐는지 확인합니다. 아래 명령에서 `invalid access code`가 나오면 서버가 제대로 떠 있는 겁니다.

```bash
curl -X POST https://teamtalk-relay.<내계정이름>.workers.dev/v1/messages
```

접속 코드까지 확인하려면 아래를 실행합니다. 짧은 AI 답변이 JSON으로 오면 성공입니다.

```bash
curl -X POST https://teamtalk-relay.<내계정이름>.workers.dev/v1/messages -H "x-teamtalk-token: me-x8k2p9" -H "content-type: application/json" -H "anthropic-version: 2023-06-01" -d "{\"model\":\"claude-opus-5-5\",\"max_tokens\":64,\"messages\":[{\"role\":\"user\",\"content\":\"안녕\"}]}"
```

## 6. 꼭 해둘 것: 사용 한도

https://console.anthropic.com 에서 이 서버 전용 키가 속한 워크스페이스에 **월 사용 한도(Spend limit)**를 걸어두세요. 접속 코드가 새어 나가도 피해가 그 금액 안에서 멈춥니다.

## 7. 게임에 서버 주소 넣기

게임 파일이 이 서버를 쓰도록 주소를 넣어서 다시 빌드합니다.

1. GitHub에서 레포를 열고 **Settings → Secrets and variables → Actions** 로 갑니다.
2. **Variables** 탭(Secrets 아님)에서 **New repository variable**을 누릅니다.
3. Name에 `RELAY_URL`, Value에 5번에서 복사한 주소(`https://...workers.dev`)를 넣고 저장합니다.
4. 레포의 **Actions** 탭 → 왼쪽의 **release** → 오른쪽 **Run workflow** → version에 새 버전(예: `v0.3.1`)을 넣고 실행합니다.
5. 2~3분 뒤 **Releases**에 새 파일이 올라옵니다.

이제 새 파일을 받은 사람은 혼자 하는 게임을 처음 열 때 API 키 대신 접속 코드를 묻습니다. 같이하기에서 방에 참가만 하는 사람은 아무것도 입력하지 않아도 됩니다.

서버 주소는 비밀이 아닙니다. 접속 코드가 없으면 아무것도 할 수 없습니다.

---

## 운영

**누군가의 접속 코드를 끊고 싶을 때:** 3번 명령으로 그 사람 코드를 뺀 목록을 다시 넣습니다. 즉시 적용됩니다.

**사용량 보기:** Cloudflare 대시보드 → Workers & Pages → teamtalk-relay에서 요청 수를 볼 수 있습니다. 비용은 Anthropic 콘솔의 Usage에서 확인합니다.

**허용 모델 바꾸기:** `wrangler.toml`의 `ALLOWED_MODELS`를 고치고 `npx wrangler deploy`를 다시 실행합니다. 예를 들어 비용을 줄이려고 대화를 Haiku로 돌리려면 `ALLOWED_MODELS = "claude-opus-5-5,claude-haiku-5-5"`로 바꾸고, 게임은 `TEAMTALK_NPC_MODEL=claude-haiku-5-5`로 실행합니다.

**서버 코드를 고친 뒤:** `relay/` 폴더에서 `npx wrangler deploy`만 다시 하면 됩니다. 비밀값은 그대로 유지됩니다.

## 서버가 막아주는 것

- 접속 코드가 없거나 틀리면 AI 호출과 방 만들기를 거절합니다
- `ALLOWED_MODELS`에 있는 모델만 쓸 수 있습니다
- 요청 하나의 `max_tokens`를 `MAX_TOKENS`로 제한합니다
- 요청에 들어온 tools 같은 필드는 빼고 전달합니다
- 접속 코드별 하루 요청 수를 제한합니다 (4번을 했을 때)
- 방 하나에 참가자는 8개 연결까지만 받습니다 (게임은 4명까지 허용)
