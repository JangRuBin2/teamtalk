# TeamTalk — 사내 메신저 추리 게임

겉보기엔 평범한 사내 메신저 CLI입니다. 실제로는 오전 9시에 팀장에게 사건 조사를 맡고, **18:00 퇴근 전까지** 동료 5명에게 DM을 보내 범인을 찾는 게임입니다.

- 판마다 Claude가 새 사건, 동료 성격, 알리바이, 증거를 만듭니다.
- 동료 각자가 AI입니다. 범인은 거짓말을 하고, 무고한 동료도 숨기는 비밀이 있어서 수상해 보입니다.
- 메시지 1개에 10분, 증거를 처음 열람하면 20분이 흐릅니다. 12시가 되면 점심시간으로 1시간이 넘어갑니다.
- 진행 상황은 자동 저장되고, `/b`를 누르면 가짜 빌드 로그 화면으로 바뀝니다(보스키).

## 실행

| OS | 파일 |
|---|---|
| Windows | `teamtalk-windows.exe` (더블클릭 또는 터미널에서 실행) |
| Mac (M1~M4) | `teamtalk-mac-applesilicon` |
| Mac (Intel) | `teamtalk-mac-intel` |
| Linux | `teamtalk-linux` |

Mac은 인터넷에서 받은 서명 안 된 파일을 막기 때문에, 처음 한 번만 아래를 실행하세요.

```bash
chmod +x teamtalk-mac-applesilicon
xattr -d com.apple.quarantine teamtalk-mac-applesilicon
./teamtalk-mac-applesilicon
```

Windows에서 SmartScreen 경고가 뜨면 "추가 정보 → 실행"을 누르세요.

처음 실행하면 Anthropic API 키를 물어봅니다. 입력한 키는 `~/.teamtalk/config.json`에 저장됩니다. 환경변수 `ANTHROPIC_API_KEY`가 있으면 그 값을 먼저 씁니다.

## 명령어

| 입력 | 동작 |
|---|---|
| `1`~`5` | 해당 동료와 DM 열기. 그다음 그냥 입력하면 메시지가 전송됩니다 |
| `/e`, `/e 2` | 증거 목록 보기 / 증거 열람 |
| `/memo 내용` | 메모 추가 (`/memo`만 입력하면 메모 보기) |
| `/log` | 현재 DM 전체 대화 보기 |
| `/accuse` | 범인 지목 (게임 종료, 추리 근거로 등급 S~C) |
| `/b` | 보스키 |
| `/q` | 저장하고 종료 |

## 모델 설정

기본값은 `claude-opus-5-5`입니다. 동료 대화는 낮은 effort로 호출해서 응답이 빠릅니다. 비용을 줄이고 싶으면 대화용 모델만 바꿀 수 있습니다.

```bash
TEAMTALK_NPC_MODEL=claude-haiku-5-5 ./teamtalk-linux
```

`TEAMTALK_MODEL`은 사건 생성과 채점에 쓰는 모델입니다.

## 빌드

Go 1.25 이상이 필요합니다.

```bash
./build.sh   # dist/ 에 4개 플랫폼 바이너리 생성
```
