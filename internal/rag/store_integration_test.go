//go:build integration

// 통합 테스트: 실제 PostgreSQL 의 review_history 테이블에 대해
// SaveHistory → ListHistory → GetHistory 왕복을 검증한다.
//
// 실행:  go test -tags=integration ./internal/rag   (또는 make test-integration)
// 전제:  TEST_DATABASE_URL 환경변수 + migrations/0001 적용된 DB.
//        OpenAI·sensitive_events 는 필요하지 않다(히스토리 경로만 시험).
package rag

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegration_HistoryCRUD(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 미설정 — 통합 테스트 건너뜀")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("DB 풀 생성 실패: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("DB 접속 실패: %v", err)
	}

	svc := &Service{pool: pool, store: &store{pool: pool}}

	r := &ReviewResult{
		ID:    newReviewID(),
		Input: "통합테스트용 광고 문구",
		Verdict: Verdict{
			Risky:     true,
			RiskLevel: "high",
			Score:     72,
			Reasons:   []string{"테스트 사유"},
			Advice:    "테스트 조언",
		},
		Highlights:    []Highlight{},
		Rewrite:       Rewrite{Before: "통합테스트용 광고 문구", After: "안전한 대체 문구"},
		RelatedTopics: []Topic{},
		RelatedIssues: []RelatedItem{},
	}
	// 테스트 종료 시 삽입한 행 정리.
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM review_history WHERE id = $1", r.ID) }()

	// 1) 저장
	if err := svc.SaveHistory(ctx, r, "text", 1234); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}

	// 2) 목록 조회 — 방금 저장한 id 가 포함되어야 한다.
	items, total, err := svc.ListHistory(ctx, 50, 0)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if total < 1 {
		t.Fatalf("total = %d, want >= 1", total)
	}
	found := false
	for _, it := range items {
		if it.ID == r.ID {
			found = true
			if it.Status != "needs_review" { // score 72 → high → needs_review
				t.Errorf("status = %q, want needs_review", it.Status)
			}
			if it.Score != 72 {
				t.Errorf("score = %d, want 72", it.Score)
			}
		}
	}
	if !found {
		t.Fatalf("저장한 id %s 가 목록에 없음", r.ID)
	}

	// 3) 상세 조회 — 저장한 결과가 그대로 복원되어야 한다.
	got, err := svc.GetHistory(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if got.Input != r.Input {
		t.Errorf("input = %q, want %q", got.Input, r.Input)
	}
	if got.Verdict.Score != r.Verdict.Score {
		t.Errorf("score = %d, want %d", got.Verdict.Score, r.Verdict.Score)
	}
	if got.Rewrite.After != r.Rewrite.After {
		t.Errorf("rewrite.after = %q, want %q", got.Rewrite.After, r.Rewrite.After)
	}
}
