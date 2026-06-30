package rag

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
)

// fakeAI 는 aiClient 를 만족하는 결정적 가짜 구현이다.
type fakeAI struct {
	rewrite    string
	embed      []float32
	embedBatch [][]float32
	judge      string
	generate   []string
	embedErr   error
	judgeErr   error
	genErr     error
}

func (f fakeAI) Rewrite(ctx context.Context, q string) string { return q + " " + f.rewrite }
func (f fakeAI) Embed(ctx context.Context, t string) ([]float32, error) {
	return f.embed, f.embedErr
}
func (f fakeAI) EmbedBatch(ctx context.Context, ts []string) ([][]float32, error) {
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	// 입력 개수만큼 동일 벡터를 돌려준다.
	out := make([][]float32, len(ts))
	for i := range ts {
		out[i] = []float32{0.1, 0.2, 0.3}
	}
	return out, nil
}
func (f fakeAI) Judge(ctx context.Context, sys, user string) (string, error) {
	return f.judge, f.judgeErr
}
func (f fakeAI) Generate(ctx context.Context, p, tone string, tr []string) ([]string, error) {
	return f.generate, f.genErr
}

func TestSyncKnowledge_BackfillsNullAcrossTables(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	// sensitive_events: NULL 2건 → 임베딩 후 UPDATE 2회
	mock.ExpectQuery("FROM sensitive_events WHERE embedding IS NULL").
		WillReturnRows(pgxmock.NewRows([]string{"pk", "text"}).
			AddRow("e1", "광복절 1945").
			AddRow("e2", "세월호 2014"))
	mock.ExpectExec("UPDATE sensitive_events SET embedding").
		WithArgs(pgxmock.AnyArg(), "e1").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("UPDATE sensitive_events SET embedding").
		WithArgs(pgxmock.AnyArg(), "e2").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// 나머지 테이블은 NULL 0건 (issues → slang → mim 순서)
	mock.ExpectQuery("FROM sensitive_issues WHERE embedding IS NULL").
		WillReturnRows(pgxmock.NewRows([]string{"pk", "text"}))
	mock.ExpectQuery("FROM slang_terms WHERE embedding IS NULL").
		WillReturnRows(pgxmock.NewRows([]string{"pk", "text"}))
	mock.ExpectQuery("FROM mim_terms WHERE embedding IS NULL").
		WillReturnRows(pgxmock.NewRows([]string{"pk", "text"}))
	// 동기화 시각 갱신
	mock.ExpectQuery("INSERT INTO kb_sync_meta").
		WillReturnRows(pgxmock.NewRows([]string{"last_synced_at"}).AddRow(time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)))

	s := &Service{pool: mock, ai: fakeAI{}}
	res, err := s.SyncKnowledge(context.Background())
	if err != nil {
		t.Fatalf("SyncKnowledge: %v", err)
	}
	if res.Synced != 2 || res.ByTable["sensitive_events"] != 2 || res.ByTable["mim_terms"] != 0 {
		t.Fatalf("res = %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestKnowledgeStatus_PerTable(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	ts := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery("last_synced_at FROM kb_sync_meta").
		WillReturnRows(pgxmock.NewRows([]string{"last_synced_at"}).AddRow(&ts))
	// embedSpecs 순서대로 테이블별 (total, embedded)
	mock.ExpectQuery("FROM sensitive_events").
		WillReturnRows(pgxmock.NewRows([]string{"total", "embedded"}).AddRow(96, 96))
	mock.ExpectQuery("FROM sensitive_issues").
		WillReturnRows(pgxmock.NewRows([]string{"total", "embedded"}).AddRow(69, 69))
	mock.ExpectQuery("FROM slang_terms").
		WillReturnRows(pgxmock.NewRows([]string{"total", "embedded"}).AddRow(1149, 1149))
	mock.ExpectQuery("FROM mim_terms").
		WillReturnRows(pgxmock.NewRows([]string{"total", "embedded"}).AddRow(688, 0))

	s := &Service{pool: mock}
	st, err := s.KnowledgeStatus(context.Background())
	if err != nil {
		t.Fatalf("KnowledgeStatus: %v", err)
	}
	if st.Tables["sensitive_events"].Total != 96 || st.Tables["sensitive_events"].Embedded != 96 {
		t.Fatalf("events status = %+v", st.Tables["sensitive_events"])
	}
	if st.Tables["mim_terms"].Total != 688 || st.Tables["mim_terms"].Embedded != 0 {
		t.Fatalf("mim status = %+v", st.Tables["mim_terms"])
	}
	if st.LastSyncedAt == "" {
		t.Fatalf("last_synced 미설정")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestTrends_DelegatesToStore(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.ExpectQuery("FROM mim_terms").
		WithArgs(12).
		WillReturnRows(pgxmock.NewRows([]string{"word", "definition", "trend_score", "search_ratios_90d"}).AddRow("중꺾마", "중간에 꺾여도 그냥 한다", 9.9, []int32{50, 50}))

	s := &Service{pool: mock, store: &store{pool: mock}}
	got, err := s.Trends(context.Background(), 12)
	if err != nil || len(got) != 1 || got[0].Tag != "#중꺾마" {
		t.Fatalf("Trends: %v %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
