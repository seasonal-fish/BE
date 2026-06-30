# RUN.md — 실행·재현 가이드

제출물(이 레포)만으로 빈 환경에서 백엔드를 다시 띄울 수 있도록 작성한 문서입니다.
평가 Agent의 **DevOps 8개 기준**에 1:1로 대응합니다.

| # | 기준 | 충족 위치 |
|---|---|---|
| 1 | 라이브러리 목록 명시 | `go.mod` / `go.sum` |
| 2 | 라이브러리 버전 고정 | `go.mod` (모든 의존성 버전 고정) |
| 3 | 빌드·설치 방법 | [3. 빌드](#3-빌드설치) |
| 4 | 실행·기동 방법 | [4. 실행](#4-실행기동) |
| 5 | 환경 변수·설정 | [5. 환경 변수](#5-환경-변수) · `.env.example` |
| 6 | 외부 서비스·자원 | [6. 외부 자원](#6-외부-서비스자원) |
| 7 | 문서대로 빌드 성공 | [3](#3-빌드설치) (검증됨) |
| 8 | 애플리케이션 기동 | [4](#4-실행기동) + [7. 동작 확인](#7-동작-확인) |

---

## 0. 사전 준비물

- **Go 1.26+** (`go.mod` 의 `go 1.26.4` 기준)
- **PostgreSQL 15** + **pgvector 0.7+** 확장
- **OpenAI API 키** (임베딩·판정 호출)
- (선택) Docker / docker-compose — 컨테이너 실행 시

## 1~2. 라이브러리 (목록·버전 고정)

Go 모듈로 관리되며 `go.mod` 에 모든 의존성과 **버전이 고정**되어 있습니다. 별도 설치 명령은 빌드 시 자동 수행됩니다.

```bash
go mod download   # (선택) 의존성 미리 받기
```

## 3. 빌드·설치

```bash
# 로컬 바이너리
go build -o bin/server ./cmd/server

# 또는 도커 이미지 (멀티스테이지, 정적 바이너리)
docker build -t be:local .
```

## 4. 실행·기동

### 4-1. DB 마이그레이션 (최초 1회)

`migrations/` 의 SQL 을 **번호 순서대로** 적용합니다. 스키마(0003)와 시드(0004, 임베딩 포함)까지 적용하면 RAG 검토가 바로 동작합니다.

```bash
set -a; . ./.env; set +a                      # DATABASE_URL 로드
for f in migrations/*.sql; do
  echo "applying $f"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$f"
done
```

| 파일 | 내용 |
|---|---|
| `0001_review_history.sql` | 검토 히스토리 테이블 |
| `0002_kb_sync_meta.sql` | 지식베이스 동기화 메타 |
| `0003_sensitive_schema.sql` | 민감 주제/전례 테이블 + `vector` 확장 + HNSW 인덱스 |
| `0004_sensitive_seed.sql` | 민감 주제 96건·전례 69건 시드(**임베딩 1536d 포함**) |
| `0005_slang_mim_schema.sql` | 신조어(`slang_terms`)·유행어(`mim_terms`) 테이블 + 임베딩 컬럼·HNSW (멱등) |

> 시드에 임베딩이 들어 있어 `/knowledge/sync`(OpenAI 호출) 없이도 `/review` 의 벡터 검색이 동작합니다.

### 4-3. 임베딩 채우기 (임베딩 컬럼이 빈 경우)

임베딩 대상 테이블: `sensitive_events`·`sensitive_issues`·`slang_terms`·`mim_terms`.

- **NULL만 백필 (운영용, idempotent)** — `POST /knowledge/sync` 가 전 테이블의 `embedding IS NULL` 행만 임베딩한다. 신규 행 보강에 쓴다.
  ```bash
  curl -X POST localhost:8080/knowledge/sync   # 응답 by_table 에 테이블별 신규 건수
  curl localhost:8080/knowledge/status         # 테이블별 total/embedded 현황
  ```
- **전체 재임베딩 (수동, 일회성)** — 리포 밖 `../reembed_all.py` 가 전 행을 다시 임베딩한다(NULL 필터 없음). 사용법은 해당 파일 상단 docstring 참고(터널 + `DATABASE_URL`·`OPENAI_API_KEY` env).

### 4-2. 서버 기동

```bash
# 로컬
set -a; . ./.env; set +a
go run ./cmd/server            # http://localhost:8080

# 또는 도커
docker run --rm -p 8080:8080 --env-file .env be:local

# 또는 운영용 compose (ECR 이미지 pull)
docker-compose up -d
```

## 5. 환경 변수

`.env.example` 을 복사해 `.env` 를 만들고 값을 채웁니다. (`.env` 는 `.gitignore` 로 커밋 제외)

```bash
cp .env.example .env
```

| 변수 | 필수 | 설명 |
|---|---|---|
| `DATABASE_URL` | ✅ | `postgres://user:pass@host:5432/db` (pgvector 확장 필요) |
| `OPENAI_API_KEY` | ✅ | OpenAI API 키. 미설정 시 서버가 기동 시점에 종료됨 |

## 6. 외부 서비스·자원

- **PostgreSQL 15 + pgvector** — 민감 주제/전례 저장 및 벡터 유사도 검색. `0003` 이 `CREATE EXTENSION IF NOT EXISTS vector;` 를 포함합니다.
- **OpenAI API** — 임베딩(`text-embedding-3-small`, 1536d)과 판정/생성(`gpt-4o-mini`). 네트워크 아웃바운드(TLS) 필요.
- (배포) **AWS ECR/EC2** — `docker-compose.yml` 과 `.github/workflows/deploy.yml` 참고. 키는 GitHub Secrets 로 주입됩니다(레포에 미포함).

## 7. 동작 확인

```bash
# 8. 기동 확인
curl localhost:8080/health
# {"status":"ok"}

# 검토 (DB 마이그레이션·시드 + OPENAI_API_KEY 필요)
curl -X POST localhost:8080/review \
  -H 'Content-Type: application/json' \
  -d '{"text":"8월 15일 광복절 기념 한정 세일"}'
```

## 8. 테스트

```bash
make test              # 유닛 테스트 (DB·OpenAI 불필요)
make test-integration  # 통합 테스트 — 환경변수 TEST_DATABASE_URL 필요
make test-junit        # test-results/junit.xml + coverage.out 생성 (gotestsum 필요)
```

`gotestsum` 미설치 시: `go install gotest.tools/gotestsum@latest`
