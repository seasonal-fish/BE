package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// HistoryItem 은 검토 히스토리 목록의 한 행입니다(UI 바인딩 전용 축약 뷰).
type HistoryItem struct {
	ID      string `json:"id"`
	Date    string `json:"date"` // created_at 의 RFC3339 문자열
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Src     string `json:"src"`
	Status  string `json:"status"`
	Score   int    `json:"score"`
}

// HistoryStatCard 는 히스토리 화면 상단 요약 카드 한 장입니다.
type HistoryStatCard struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit"`
	Sub   string `json:"sub"`
}

// truncateRunes 는 문자열을 rune 기준 n자로 자릅니다. 길면 말줄임표(…)를 붙입니다.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// SaveHistory 는 검토 결과 전체(ReviewResult)를 review_history 에 저장합니다.
// title/snippet 은 input 에서 잘라내고, result 컬럼에는 r 전체를 JSON 으로 저장합니다.
// 같은 id 가 이미 있으면 무시합니다(ON CONFLICT DO NOTHING).
func (s *Service) SaveHistory(ctx context.Context, r *ReviewResult, source string, latencyMs int) error {
	if r == nil {
		return fmt.Errorf("저장할 검토 결과가 nil 입니다")
	}

	// status: score(=RiskLevel) 기반 단일 진실원천 statusLabel 로 결정.
	// medium 이상이면 needs_review, 그 외는 reviewed.
	status := statusLabel(r.Verdict.Score)

	title := truncateRunes(r.Input, 40)
	snippet := truncateRunes(r.Input, 80)

	resultJSON, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("검토 결과 직렬화 실패: %w", err)
	}

	const q = `
		INSERT INTO review_history (id, title, input, snippet, source, status, score, result, latency_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING`
	if _, err := s.pool.Exec(ctx, q, r.ID, title, r.Input, snippet, source, status, r.Verdict.Score, resultJSON, latencyMs); err != nil {
		return fmt.Errorf("검토 히스토리 저장 실패: %w", err)
	}
	return nil
}

// ListHistory 는 created_at 내림차순으로 히스토리를 페이징 조회하고 전체 건수도 반환합니다.
func (s *Service) ListHistory(ctx context.Context, limit, offset int) (items []HistoryItem, total int, err error) {
	const countQ = `SELECT COUNT(*) FROM review_history`
	if err := s.pool.QueryRow(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("검토 히스토리 건수 조회 실패: %w", err)
	}

	const q = `
		SELECT id, created_at, title, snippet, source, status, score
		FROM review_history
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`
	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("검토 히스토리 목록 조회 실패: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var it HistoryItem
		var createdAt time.Time
		if err := rows.Scan(&it.ID, &createdAt, &it.Title, &it.Snippet, &it.Src, &it.Status, &it.Score); err != nil {
			return nil, 0, fmt.Errorf("검토 히스토리 행 스캔 실패: %w", err)
		}
		it.Date = createdAt.Format(time.RFC3339)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("검토 히스토리 순회 실패: %w", err)
	}
	return items, total, nil
}

// GetHistory 는 id 로 저장된 검토 결과(result jsonb)를 ReviewResult 로 복원해 반환합니다.
func (s *Service) GetHistory(ctx context.Context, id string) (*ReviewResult, error) {
	const q = `SELECT result FROM review_history WHERE id = $1`
	var raw []byte
	if err := s.pool.QueryRow(ctx, q, id).Scan(&raw); err != nil {
		return nil, fmt.Errorf("검토 히스토리 조회 실패: %w", err)
	}

	var r ReviewResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("검토 결과 역직렬화 실패: %w", err)
	}
	return &r, nil
}

// HistoryStats 는 히스토리 화면 상단 요약 카드 3개를 계산해 반환합니다.
// 데이터가 없으면 모든 값을 0 으로 채웁니다.
func (s *Service) HistoryStats(ctx context.Context) ([]HistoryStatCard, error) {
	// '위험 검출' 카드 라벨과 일치시키기 위해 safetyLabel 의 '위험'(high, score>=67)만
	// 집계한다. medium(score 34~66)은 '주의' 라벨이므로 위험 검출에 넣지 않는다.
	// 인덱싱 가능한 score 컬럼을 써서 jsonb 추출보다 빠르게 계산한다.
	const q = `
		SELECT
			COUNT(*)                                 AS total,
			COALESCE(AVG(latency_ms), 0)             AS avg_latency,
			COUNT(*) FILTER (WHERE score >= 67)      AS risky
		FROM review_history`
	var total, risky int
	var avgLatency float64
	if err := s.pool.QueryRow(ctx, q).Scan(&total, &avgLatency, &risky); err != nil {
		return nil, fmt.Errorf("검토 히스토리 통계 조회 실패: %w", err)
	}

	// 평균 응답: 밀리초 -> 초, 소수 1자리
	avgSeconds := avgLatency / 1000.0

	// 위험 검출 비율(%): high+medium / total
	var riskRatio float64
	if total > 0 {
		riskRatio = float64(risky) / float64(total) * 100.0
	}

	cards := []HistoryStatCard{
		{Label: "평균 응답", Value: fmt.Sprintf("%.1f", avgSeconds), Unit: "초", Sub: ""},
		{Label: "총 검토", Value: fmt.Sprintf("%d", total), Unit: "건", Sub: ""},
		{Label: "위험 검출", Value: fmt.Sprintf("%.0f", riskRatio), Unit: "%", Sub: ""},
	}
	return cards, nil
}
