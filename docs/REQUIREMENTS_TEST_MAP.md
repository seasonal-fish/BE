# 요구사항 ↔ 코드 ↔ 테스트 추적표 (백엔드)

요구사항 명세서(R-ID)와 백엔드 구현·자동 테스트의 매핑. 평가 Agent의 5.1(기능 구현)·5.2(테스트) 추적을 돕기 위한 문서.

> 범위 주의: 이 레포는 `program_backend` 다. UI(입력 폼·하이라이트 렌더·리포트 화면)는 `program_frontend`, 데이터 수집·신조어는 데이터 파이프라인 프로그램 책임이다. 아래는 **백엔드가 책임지는 부분**만 표기한다.

## 매핑

| R-ID | 백엔드 구현 | 자동 테스트 | 종류 |
|---|---|---|---|
| R-01 입력 수신 | `handler/review.go` (`POST /review` 검증) | `TestReview_Validation`, `TestHealth_OK` | 유닛 |
| R-02 위험 구간 하이라이트 | `rag/service.go` `Highlight`, `review_helpers.go:phraseOffsets` | `TestPhraseOffsets` | 유닛 |
| R-03 판정 근거 | `rag/service.go` `Verdict.Reasons` / `Highlight.Reason,Basis` | (판정은 LLM 의존 — 통합/시연으로 입증) | — |
| R-04 대체 문구 | `rag/service.go` `Rewrite.After` / `Highlight.Alt` | (LLM 의존) | — |
| R-05 RAG 벡터 검색 | `rag/store.go:searchVector`, `rag/index.go:fuseRank` | `TestFuseRank`, `TestLexScore`, `TestArgsortDesc`, `TestRanks`, `TestDateSet`, `TestVectorLiteral` | 유닛 |
| R-08 needs_review 분류 | `rag/review_helpers.go:statusLabel/normalizeSeverity` | `TestStatusLabel`, `TestNormalizeSeverity`, `TestScoreToLevel` | 유닛 |
| R-10 시드 DB | `migrations/0003·0004` (events 96·issues 69) | (마이그레이션 적용 검증) | 수동/통합 |
| R-15 임베딩 저장 | `rag/knowledge.go:SyncKnowledge` | `TestClampScore` 등 보조 | 유닛 |
| R-19 응답 시간 측정 | `handler/review.go` latency, `rag/history.go:HistoryStats` | `TestIntegration_HistoryCRUD` (저장·집계 경로) | 통합 |
| 히스토리 저장/조회 | `rag/history.go` Save/List/Get | `TestIntegration_HistoryCRUD`, `TestTruncateRunes`, `TestNewReviewID` | 통합/유닛 |
| 안전 라벨/노트 | `rag/review_helpers.go:safetyLabel`, `generate.go:safetyNote` | `TestSafetyLabel`, `TestSafetyNote` | 유닛 |
| 입력 검증/CORS/라우팅 | `handler/*`, `router/router.go` | `TestReview_Validation`, `TestGenerate_Validation`, `TestCORS_Preflight`, `TestUnknownRoute_404` | 유닛 |

## 테스트 종류

- **유닛**: 의존성 없이 실행. `make test` / `go test ./...`
- **통합**: 실제 PostgreSQL 필요. `TEST_DATABASE_URL` 설정 후 `make test-integration`. 미설정 시 자동 skip.
- **LLM 의존(R-03/04 판정 내용)**: OpenAI 호출 결과라 결정적 단위 테스트 대상이 아니며, 시연 영상·실행 로그로 입증한다.

## 커버리지 (statements)

| 패키지 | 커버리지 | 방식 |
|---|---|---|
| `internal/handler` | 100% | gin httptest + 가짜 Service 주입 |
| `internal/router` | 100% | gin httptest |
| `internal/rag` | ~85% | OpenAI httptest 스텁 + pgxmock(DB) + 가짜 aiClient |
| **전체** | **86.4%** | (entrypoint `cmd/server/main.go` 제외 시 더 높음) |

DB·OpenAI 의존 코드는 인터페이스(`pgxPool`/`aiClient`)로 추상화해 **인프라 없이 결정적으로** 검증한다. 실제 DB 왕복은 `-tags=integration` 통합 테스트가 별도로 담당한다.

## 결과 산출물

`make test-junit` → `test-results/junit.xml` (표준 JUnit 포맷, 평가 Agent가 테스트명·통과/실패로 매칭) + `coverage.out`.
