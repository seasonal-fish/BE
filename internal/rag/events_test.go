package rag

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
)

func TestYearFromDate(t *testing.T) {
	cases := map[string]string{
		"2017-04-04": "2017",
		"1987":       "1987",
		"1987년":      "1987",
		"":           "",
		"abc":        "",
		"12-34":      "", // 4자리 연속 숫자가 없으면 빈 값
		"2014.04.16": "2014",
	}
	for in, want := range cases {
		if got := yearFromDate(in); got != want {
			t.Errorf("yearFromDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListEvents_MapsRowsAndCount(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM sensitive_events").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery("FROM sensitive_events").
		WithArgs(20, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id", "title", "category", "event_date", "issue_count"}).
			AddRow("evt_1", "박종철 사건", "역사·인권", "1987-01-14", 3).
			AddRow("evt_2", "세월호", "재난", "2014", 0))

	s := &store{pool: mock}
	got, total, err := s.listEvents(context.Background(), 20, 0)
	if err != nil {
		t.Fatalf("listEvents: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(got) != 2 || got[0].ID != "evt_1" || got[0].Year != "1987" || got[0].IssueCount != 3 {
		t.Fatalf("row0 = %+v", got[0])
	}
	if got[1].Year != "2014" {
		t.Errorf("row1 year = %q, want 2014", got[1].Year)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestService_GetEvent_AttachesIssues(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	// 1) 사건 본체
	mock.ExpectQuery("FROM sensitive_events").
		WithArgs("evt_1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "title", "category", "event_date", "description"}).
			AddRow("evt_1", "박종철 사건", "역사·인권", "1987-01-14", "고문치사 사건"))
	// 2) 연결 전례
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs("evt_1").
		WillReturnRows(pgxmock.NewRows([]string{"issue_id", "title", "description", "issue_date"}).
			AddRow("iss_1", "A 워터파크 광고", "여름 프로모션", (*time.Time)(nil)))

	svc := &Service{store: &store{pool: mock}}
	got, err := svc.GetEvent(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got.ID != "evt_1" || got.Year != "1987" || got.Description != "고문치사 사건" {
		t.Fatalf("detail = %+v", got)
	}
	if len(got.Issues) != 1 || got.Issues[0].ID != "iss_1" || got.Issues[0].Brand != "A 워터파크 광고" {
		t.Fatalf("issues = %+v", got.Issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestService_GetEvent_NoIssuesNormalized(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.ExpectQuery("FROM sensitive_events").
		WithArgs("evt_2").
		WillReturnRows(pgxmock.NewRows([]string{"id", "title", "category", "event_date", "description"}).
			AddRow("evt_2", "사건", "", "2014", ""))
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs("evt_2").
		WillReturnRows(pgxmock.NewRows([]string{"issue_id", "title", "description", "issue_date"}))

	svc := &Service{store: &store{pool: mock}}
	got, err := svc.GetEvent(context.Background(), "evt_2")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	// nil 이 아니라 빈 슬라이스로 정규화되어 JSON 에서 [] 로 나가야 한다.
	if got.Issues == nil || len(got.Issues) != 0 {
		t.Fatalf("Issues = %#v, want 빈 슬라이스", got.Issues)
	}
}

func TestService_ListEvents_Delegates(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM sensitive_events").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("FROM sensitive_events").
		WithArgs(20, 0).
		WillReturnRows(pgxmock.NewRows([]string{"id", "title", "category", "event_date", "issue_count"}).
			AddRow("evt_1", "사건", "역사", "1987", 2))

	svc := &Service{store: &store{pool: mock}}
	got, total, err := svc.ListEvents(context.Background(), 20, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].Year != "1987" {
		t.Fatalf("got=%+v total=%d", got, total)
	}
}

func TestTrendingTerms_AssignsRank(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	mock.ExpectQuery("FROM mim_terms").
		WithArgs(12).
		WillReturnRows(pgxmock.NewRows([]string{"word", "definition", "trend_score", "search_ratios_90d"}).
			AddRow("중꺾마", "중간에 꺾여도 그냥 한다", 9.9, []int32{50, 50}).
			AddRow("단호박", "단호한 사람", 9.8, []int32{40, 40}).
			AddRow("에타", "에브리타임", 9.3, []int32{30, 30}))

	s := &store{pool: mock}
	got, err := s.trendingTerms(context.Background(), 12)
	if err != nil {
		t.Fatalf("trendingTerms: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, tr := range got {
		if tr.Rank != i+1 {
			t.Errorf("got[%d].Rank = %d, want %d", i, tr.Rank, i+1)
		}
		if tr.Delta != 0 {
			t.Errorf("got[%d].Delta = %d, want 0(평탄한 추이)", i, tr.Delta)
		}
	}
	if got[0].Tag != "#중꺾마" {
		t.Errorf("Tag = %q, want #중꺾마", got[0].Tag)
	}
	if got[0].Up != 9.9 {
		t.Errorf("Up = %v, want 9.9", got[0].Up)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestGetEvent_NotFound(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	mock.ExpectQuery("FROM sensitive_events").
		WithArgs("missing").
		WillReturnError(pgx.ErrNoRows)

	s := &store{pool: mock}
	_, err := s.getEvent(context.Background(), "missing")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("err = %v, want pgx.ErrNoRows 래핑", err)
	}
}

func TestIssuesForEvent_MapsRows(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()

	d := time.Date(2017, 4, 4, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery("FROM sensitive_issues").
		WithArgs("evt_1").
		WillReturnRows(pgxmock.NewRows([]string{"issue_id", "title", "description", "issue_date"}).
			AddRow("INTL-001", "펩시 켄달 제너 광고 논란", "BLM 시위 모티브 광고", &d).
			AddRow("INTL-002", "날짜 없는 사례", "설명", (*time.Time)(nil)))

	s := &store{pool: mock}
	got, err := s.issuesForEvent(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("issuesForEvent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// brand←title, copy←description, year←issue_date
	if got[0].ID != "INTL-001" || got[0].Brand != "펩시 켄달 제너 광고 논란" ||
		got[0].Copy != "BLM 시위 모티브 광고" || got[0].Year != "2017" {
		t.Fatalf("row0 = %+v", got[0])
	}
	// 원천 없는 필드는 빈 값
	if got[0].Campaign != "" || got[0].Level != "" || got[0].Result != "" {
		t.Errorf("빈 값이어야 할 필드가 채워짐: %+v", got[0])
	}
	// 날짜 NULL 이면 year 는 빈 값
	if got[1].Year != "" {
		t.Errorf("nil 날짜 year = %q, want \"\"", got[1].Year)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
