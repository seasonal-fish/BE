package rag

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// searchVectorRows 는 searchVector 가 기대하는 컬럼으로 1건을 반환한다.
func searchVectorRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{"id", "title", "category", "event_date", "trigger_expressions", "description", "similarity"}).
		AddRow("e1", "광복절", "HISTORY", "08-15", []byte(`["광복절"]`), "1945년 광복", 0.9)
}

// relatedRows 는 searchSimilar 가 기대하는 컬럼(id,title,category,snippet,similarity)으로 1건을 반환한다.
func relatedRows(id, title, cat, snip string, sim float64) *pgxmock.Rows {
	return pgxmock.NewRows([]string{"id", "title", "category", "snippet", "similarity"}).
		AddRow(id, title, cat, snip, sim)
}

const judgeJSON = `{
  "score": 80,
  "reasons": ["역사적 사건을 가볍게 소비"],
  "advice": "표현을 순화하세요",
  "evidence_ids": ["T1", "I1", "S1", "M1"],
  "highlights": [{"phrase":"광복절","severity":"high","reason":"역사 민감","confidence":0.9}],
  "rewrite": {"after": "여름 한정 세일"}
}`

func TestReview_EndToEnd(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.MatchExpectationsInOrder(false) // 4개 테이블 검색이 goroutine 으로 병렬 실행

	mock.ExpectQuery("FROM sensitive_events").
		WithArgs(pgxmock.AnyArg(), poolSize).
		WillReturnRows(searchVectorRows())
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("i1", "과거 광복절 마케팅 논란", "HISTORY", "설명", 0.4))
	mock.ExpectQuery("FROM slang_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("s1", "쿨데레", "중립", "차갑지만 다정", 0.3))
	mock.ExpectQuery("FROM mim_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("m1", "회전문 관광객", "", "반복 방문 외국인", 0.3))

	svc := &Service{
		pool:  mock,
		store: &store{pool: mock},
		ai:    fakeAI{embed: []float32{0.1, 0.2}, judge: judgeJSON, rewrite: "광복절"},
	}

	got, err := svc.Review(context.Background(), "광복절 기념 세일")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	// 점수는 근거에서 결정론적으로 산출(high 표현 1건 → 75). LLM 의 score(80)는 쓰지 않는다.
	if got.Verdict.Score != 75 || got.Verdict.RiskLevel != "high" || !got.Verdict.Risky {
		t.Errorf("verdict = %+v", got.Verdict)
	}
	// 하이라이트 오프셋 역매핑 ("광복절" 은 입력 맨 앞 → start 0)
	if len(got.Highlights) != 1 || got.Highlights[0].Start == nil || *got.Highlights[0].Start != 0 {
		t.Errorf("highlights = %+v", got.Highlights)
	}
	// rewrite.before 는 항상 원문, after 는 LLM 결과
	if got.Rewrite.Before != "광복절 기념 세일" || got.Rewrite.After != "여름 한정 세일" {
		t.Errorf("rewrite = %+v", got.Rewrite)
	}
	if len(got.RelatedTopics) != 1 || len(got.RelatedIssues) != 1 || len(got.RelatedSlang) != 1 || len(got.RelatedTrends) != 1 {
		t.Errorf("topics=%d issues=%d slang=%d trends=%d",
			len(got.RelatedTopics), len(got.RelatedIssues), len(got.RelatedSlang), len(got.RelatedTrends))
	}
	if len(got.RelatedIssues) > 0 && got.RelatedIssues[0].Source != "sensitive_issues" {
		t.Errorf("issue source = %q", got.RelatedIssues[0].Source)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// judgeJSONLowScore 는 전체 score 는 낮게(none 밴드) 내면서 high 하이라이트를 함께 내는,
// LLM 이 흔히 만드는 모순 응답이다. 후처리(computeScore)가 LLM 의 score(10)를 버리고
// 근거(high 표현 1건)에서 점수를 재계산해 위험 밴드(75)로 끌어올리는지 검증하기 위한 입력이다.
const judgeJSONLowScore = `{
  "score": 10,
  "reasons": ["표현 자체는 약하나 민감 사건과 연결"],
  "advice": "표현을 순화하세요",
  "highlights": [{"phrase":"광복절","severity":"high","reason":"역사 민감","confidence":0.9}],
  "rewrite": {"after": "여름 한정 세일"}
}`

func TestReview_ScoreDerivedFromEvidence(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.MatchExpectationsInOrder(false)

	mock.ExpectQuery("FROM sensitive_events").
		WithArgs(pgxmock.AnyArg(), poolSize).
		WillReturnRows(searchVectorRows())
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("i1", "과거 광복절 마케팅 논란", "HISTORY", "설명", 0.4))
	mock.ExpectQuery("FROM slang_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("s1", "쿨데레", "중립", "차갑지만 다정", 0.3))
	mock.ExpectQuery("FROM mim_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("m1", "회전문 관광객", "", "반복 방문 외국인", 0.3))

	svc := &Service{
		pool:  mock,
		store: &store{pool: mock},
		ai:    fakeAI{embed: []float32{0.1, 0.2}, judge: judgeJSONLowScore, rewrite: "광복절"},
	}

	got, err := svc.Review(context.Background(), "광복절 기념 세일")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	// LLM score=10 은 무시하고, high 표현 1건의 근거로 75(위험)를 산출해야 한다.
	if got.Verdict.Score != 75 || got.Verdict.RiskLevel != "high" || !got.Verdict.Risky {
		t.Errorf("근거기반 점수 실패: verdict = %+v, want score=75/high/risky", got.Verdict)
	}
}

// judgeJSONSafe 는 하이라이트가 없는(score 0=안전) 응답이다. 위험 표현이 없으면
// 관련 근거(topics/issues/slang/trends)를 모두 비우는지 검증하기 위한 입력이다.
const judgeJSONSafe = `{
  "score": 0,
  "reasons": ["현재 문구는 위험 표현으로 표시할 근거가 부족합니다"],
  "advice": "",
  "evidence_ids": ["T1", "I1"],
  "highlights": [],
  "rewrite": {"after": ""}
}`

func TestReview_RelatedClearedWhenSafe(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.MatchExpectationsInOrder(false)

	mock.ExpectQuery("FROM sensitive_events").
		WithArgs(pgxmock.AnyArg(), poolSize).
		WillReturnRows(searchVectorRows())
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("i1", "x", "y", "z", 0.1))
	mock.ExpectQuery("FROM slang_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("s1", "x", "y", "z", 0.1))
	mock.ExpectQuery("FROM mim_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("m1", "x", "y", "z", 0.1))

	svc := &Service{
		pool:  mock,
		store: &store{pool: mock},
		ai:    fakeAI{embed: []float32{0.1, 0.2}, judge: judgeJSONSafe, rewrite: "늙크크"},
	}

	got, err := svc.Review(context.Background(), "늙크크")
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	// 하이라이트가 없으므로 score=0(안전). evidence_ids 가 지목돼도 위험 표현이 없으면
	// 관련 근거는 모두 비운다.
	if got.Verdict.Score != 0 {
		t.Errorf("score = %d, want 0", got.Verdict.Score)
	}
	if len(got.RelatedTopics) != 0 || len(got.RelatedIssues) != 0 ||
		len(got.RelatedSlang) != 0 || len(got.RelatedTrends) != 0 {
		t.Errorf("안전인데 관련 근거가 비지 않음: topics=%d issues=%d slang=%d trends=%d",
			len(got.RelatedTopics), len(got.RelatedIssues), len(got.RelatedSlang), len(got.RelatedTrends))
	}
}

func TestReview_EmbedError(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	svc := &Service{
		pool:  mock,
		store: &store{pool: mock},
		ai:    fakeAI{embedErr: context.DeadlineExceeded},
	}
	if _, err := svc.Review(context.Background(), "문구"); err == nil {
		t.Fatal("임베딩 실패인데 에러가 없음")
	}
}

func TestGenerate_ReviewsCandidates(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.MatchExpectationsInOrder(false) // 후보 검토가 goroutine 으로 돌아 순서 비결정적

	mock.ExpectQuery("FROM sensitive_events").
		WithArgs(pgxmock.AnyArg(), poolSize).
		WillReturnRows(searchVectorRows())
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("i1", "과거 광복절 마케팅 논란", "HISTORY", "설명", 0.4))
	mock.ExpectQuery("FROM slang_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("s1", "쿨데레", "중립", "차갑지만 다정", 0.3))
	mock.ExpectQuery("FROM mim_terms").
		WithArgs(pgxmock.AnyArg(), relatedK).
		WillReturnRows(relatedRows("m1", "회전문 관광객", "", "반복 방문", 0.3))
	mock.ExpectExec("INSERT INTO review_history").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	svc := &Service{
		pool:  mock,
		store: &store{pool: mock},
		ai:    fakeAI{embed: []float32{0.1}, judge: judgeJSON, rewrite: "광복절", generate: []string{"광복절 기념 에코백"}},
	}

	got, err := svc.Generate(context.Background(), GenerateRequest{Product: "에코백", Tone: "위트"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	// 근거(high 표현 1건) → score 75, 안전 라벨 '위험'
	if got[0].Score != 75 || got[0].SafetyLabel != "위험" {
		t.Errorf("candidate = %+v", got[0])
	}
	if got[0].ReviewID == "" {
		t.Errorf("review_id 미설정")
	}
}

func TestGenerate_AIError(t *testing.T) {
	svc := &Service{ai: fakeAI{genErr: context.DeadlineExceeded}}
	if _, err := svc.Generate(context.Background(), GenerateRequest{Product: "p"}); err == nil {
		t.Fatal("생성 실패인데 에러가 없음")
	}
}
