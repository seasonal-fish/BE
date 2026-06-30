# BE — 요즘애드 백엔드 (광고 카피 리스크 검토 API)

광고 문구를 입력받아, 사회적·역사적으로 민감하게 해석될 위험을 **RAG(임베딩 유사 검색 + 전례 수집 + LLM 판정)** 로 진단하는 Gin 기반 REST API.

## 빠른 시작

실행·재현 전 과정은 **[RUN.md](./RUN.md)** 를 따르세요. 요약:

```bash
cp .env.example .env          # DATABASE_URL, OPENAI_API_KEY 채우기
set -a; . ./.env; set +a      # 환경변수 로드
for f in migrations/*.sql; do psql "$DATABASE_URL" -f "$f"; done   # 스키마+시드 적용
go run ./cmd/server           # http://localhost:8080
curl localhost:8080/health    # {"status":"ok"}
```

## 요구 사항

- Go 1.26+ (`go.mod` 기준)
- PostgreSQL 15 + **pgvector 0.7+**
- OpenAI API 키 (임베딩 `text-embedding-3-small`, 판정 `gpt-4o-mini`)

## 구조

```
.
├── cmd/server/             # 엔트리포인트 (main.go)
├── internal/
│   ├── router/             # 라우트 등록 + CORS 미들웨어
│   ├── handler/            # HTTP 핸들러 (review/history/generate/knowledge/health)
│   └── rag/                # RAG 파이프라인 (검색·판정·저장·지식동기화·OpenAI 클라이언트)
├── migrations/             # DB 스키마·시드 (0001~0004, psql 로 순서대로 적용)
└── docs/api/openapi.yaml   # API 명세 (OpenAPI 3.1)
```

## API 엔드포인트

| 메서드 | 경로 | 설명 |
|---|---|---|
| GET | `/health` | 헬스 체크 |
| POST | `/review` | 광고 문구 위험도 검토 |
| GET | `/history` | 검토 히스토리 목록 (페이징) |
| GET | `/history/stats` | 히스토리 요약 카드 |
| GET | `/history/:id` | 검토 결과 상세 |
| GET | `/trends` | 트렌드어 상위 N |
| POST | `/generate` | 광고 문구 생성 + 자동 검토 |
| POST | `/knowledge/sync` | 지식베이스 임베딩 동기화 |
| GET | `/knowledge/status` | 지식베이스 상태 |

요청/응답 스키마와 예시는 [`docs/api/openapi.yaml`](./docs/api/openapi.yaml) 참고.

## 개발

```bash
make test              # 유닛 테스트
make test-integration  # 통합 테스트 (TEST_DATABASE_URL 필요)
make test-junit        # test-results/junit.xml + coverage.out 생성
make build             # 빌드
```

테스트 ↔ 요구사항 추적은 [`docs/REQUIREMENTS_TEST_MAP.md`](./docs/REQUIREMENTS_TEST_MAP.md) 참고.

## 배포

`main` 푸시 시 GitHub Actions(`.github/workflows/`)가 **테스트 → ECR 이미지 빌드/푸시 → EC2 재기동** 순으로 배포합니다. 테스트 실패 시 배포는 차단됩니다.
