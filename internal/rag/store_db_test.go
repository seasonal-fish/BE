package rag

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

func TestSearchVector_MapsRows(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id", "title", "category", "event_date", "trigger_expressions", "description", "similarity"}).
		AddRow("e1", "광복절", "HISTORY", "08-15", []byte(`["광복절","해방"]`), "1945년 광복", 0.91).
		AddRow("e2", "세월호", "DISASTER", "04-16", []byte(`["세월호"]`), "2014년 참사", 0.72)
	mock.ExpectQuery("FROM sensitive_events").
		WithArgs(pgxmock.AnyArg(), 5).
		WillReturnRows(rows)

	s := &store{pool: mock}
	got, err := s.searchVector(context.Background(), []float32{0.1, 0.2}, 5)
	if err != nil {
		t.Fatalf("searchVector: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "e1" || got[0].Similarity != 0.91 {
		t.Errorf("row0 = %+v", got[0].Topic)
	}
	if len(got[0].triggers) != 2 || got[0].triggers[0] != "광복절" {
		t.Errorf("triggers = %v", got[0].triggers)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSearchSimilar_MapsRows(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id", "title", "category", "snippet", "similarity"}).
		AddRow("i1", "스타벅스 세월호 논란", "DISASTER", "참사일 머그 출시", 0.41).
		AddRow("i2", "다른 사례", "ETC", "설명", 0.33)
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs(pgxmock.AnyArg(), 5).
		WillReturnRows(rows)

	s := &store{pool: mock}
	got, err := s.searchSimilar(context.Background(), specIssues, []float32{0.1, 0.2}, 5)
	if err != nil {
		t.Fatalf("searchSimilar: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Source != "sensitive_issues" || got[0].ID != "i1" || got[0].Similarity != 0.41 {
		t.Errorf("row0 = %+v", got[0])
	}
	if got[0].Category != "DISASTER" || got[0].Snippet != "참사일 머그 출시" {
		t.Errorf("fields = %+v", got[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestTrendingTerms_MapsRows(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	// 최근 7일 평균(10) - 직전 7일 평균(0) = 10 → Delta 10 을 검증한다.
	rising := []int32{0, 0, 0, 0, 0, 0, 0, 10, 10, 10, 10, 10, 10, 10}
	rows := pgxmock.NewRows([]string{"word", "definition", "trend_score", "search_ratios_90d"}).
		AddRow("중꺾마", "중간에 꺾여도 그냥 한다", 36.2, rising).
		AddRow("단호박", "단호한 사람", 30.2, []int32{5, 5})
	mock.ExpectQuery("FROM mim_terms").
		WithArgs(10).
		WillReturnRows(rows)

	s := &store{pool: mock}
	got, err := s.trendingTerms(context.Background(), 10)
	if err != nil {
		t.Fatalf("trendingTerms: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Tag != "#중꺾마" || got[0].Up != 36.2 || got[0].Definition != "중간에 꺾여도 그냥 한다" {
		t.Fatalf("got[0] = %+v, want Tag #중꺾마 Up 36.2 Definition 설정", got[0])
	}
	if got[0].Delta != 10 {
		t.Errorf("got[0].Delta = %d, want 10", got[0].Delta)
	}
	if len(got[0].Ratios) != 14 || got[0].Ratios[7] != 10 {
		t.Errorf("got[0].Ratios = %v, want 14개·index7=10", got[0].Ratios)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
