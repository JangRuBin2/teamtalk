# TeamTalk 중계 서버

게임이 Anthropic API를 직접 부르지 않고 이 서버를 거치게 합니다. API 키는 이 서버의 비밀값으로만 저장되고, 게임 파일에는 들어가지 않습니다. 플레이어는 내가 나눠준 **접속 코드**만 있으면 됩니다.

서버가 막아주는 것:
- 접속 코드가 없거나 틀리면 거절
- 허용한 모델(`ALLOWED_MODELS`)만 사용 가능
- 요청당 `max_tokens` 상한 (`MAX_TOKENS`)
- 접속 코드별 하루 요청 수 제한 (`DAILY_LIMIT`, KV를 연결했을 때)

## 배포 (Cloudflare 무료 플랜으로 충분)

Node.js가 필요합니다. 이 폴더(`relay/`)에서 실행하세요.

```bash
npx wrangler login
```

```bash
npx wrangler secret put ANTHROPIC_API_KEY
```

```bash
npx wrangler secret put ACCESS_TOKENS
```

`ACCESS_TOKENS`에는 쉼표로 구분한 접속 코드를 넣습니다. 예: `me-4k2p,minsu-7f3a,jiyoung-91bc`. 사람마다 다른 코드를 주면 나중에 한 명만 끊을 수 있습니다. 바꿀 때는 같은 명령을 다시 실행하면 됩니다.

하루 사용량 제한을 쓰려면 KV를 만들고, 출력된 id를 `wrangler.toml`의 주석 처리된 부분에 넣고 주석을 풉니다.

```bash
npx wrangler kv namespace create USAGE
```

배포합니다.

```bash
npx wrangler deploy
```

배포가 끝나면 `https://teamtalk-relay.<내 계정>.workers.dev` 같은 주소가 나옵니다.

## 게임에 연결

GitHub 레포의 **Settings → Secrets and variables → Actions → Variables** 에서 `RELAY_URL` 변수를 만들고 위 주소를 넣으세요. 그다음부터 릴리즈로 빌드되는 게임은 이 서버를 쓰고, 처음 실행할 때 API 키 대신 접속 코드를 묻습니다.

직접 빌드할 때는 아래처럼 하면 됩니다.

```bash
RELAY_URL=https://teamtalk-relay.<내 계정>.workers.dev ./build.sh
```

설정 메뉴에서 개인 API 키로 바꿀 수도 있습니다. 개인 키를 넣으면 중계 서버를 거치지 않습니다.

## 꼭 해둘 것

Anthropic 콘솔에서 이 서버 전용 API 키를 따로 만들고, 그 키가 속한 워크스페이스에 **월 사용 한도**를 걸어두세요. 접속 코드가 새어 나가도 피해가 한도 안에서 멈춥니다.
