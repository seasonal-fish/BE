package rag

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
)

func TestSaveHistory_Insert(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	mock.ExpectExec("INSERT INTO review_history").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	s := &Service{pool: mock}
	r := &ReviewResult{ID: "rev_1", Input: "광고 문구", Verdict: Verdict{Score: 80}}
	if err := s.SaveHistory(context.Background(), r, "text", 1200); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSaveHistory_NilResult(t *testing.T) {
	s := &Service{}
	if err := s.SaveHistory(context.Background(), nil, "text", 0); err == nil {
		t.Fatal("nil 결과인데 에러가 없음")
	}
}

func TestListHistory_CountThenList(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	mock.ExpectQuery("COUNT").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	created := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("created_at").
		WithArgs(20, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "title", "snippet", "source", "status", "score"}).
			AddRow("rev_1", created, "제목1", "스니펫1", "text", "reviewed", 10).
			AddRow("rev_2", created, "제목2", "스니펫2", "generate", "needs_review", 70))

	s := &Service{pool: mock}
	items, total, err := s.ListHistory(context.Background(), 20, 0)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d items=%d", total, len(items))
	}
	if items[1].Status != "needs_review" || items[1].Score != 70 {
		t.Errorf("item1 = %+v", items[1])
	}
	if items[0].Date == "" {
		t.Errorf("date 미설정")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestGetHistory_Unmarshals(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	want := &ReviewResult{ID: "rev_9", Input: "원문", Verdict: Verdict{Score: 55, RiskLevel: "medium"}}
	raw, _ := json.Marshal(want)
	mock.ExpectQuery("SELECT result FROM review_history").
		WithArgs("rev_9").
		WillReturnRows(pgxmock.NewRows([]string{"result"}).AddRow(raw))

	s := &Service{pool: mock}
	got, err := s.GetHistory(context.Background(), "rev_9")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if got.ID != "rev_9" || got.Verdict.Score != 55 {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestHistoryStats_Cards(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	mock.ExpectQuery("review_history").
		WillReturnRows(pgxmock.NewRows([]string{"total", "avg_latency", "risky"}).
			AddRow(10, 1500.0, 4))

	s := &Service{pool: mock}
	cards, err := s.HistoryStats(context.Background())
	if err != nil {
		t.Fatalf("HistoryStats: %v", err)
	}
	if len(cards) != 3 {
		t.Fatalf("cards = %d, want 3", len(cards))
	}
	// 평균 응답 1500ms -> 1.5초
	if cards[0].Value != "1.5" {
		t.Errorf("avg card = %q, want 1.5", cards[0].Value)
	}
	// 위험 검출 4/10 -> 40%
	if cards[2].Value != "40" {
		t.Errorf("risk card = %q, want 40", cards[2].Value)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
