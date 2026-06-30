package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Topic 은 sensitive_events(민감 주제 마스터)에서 검색된 한 건입니다.
type Topic struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Category    string  `json:"category"`
	EventDate   string  `json:"event_date"`
	Description string  `json:"description"`
	Similarity  float64 `json:"-"` // 내부 랭킹용. 응답에는 노출하지 않는다.
}

// topicData 는 융합 랭킹에 쓰는 내부 후보(키워드 포함)입니다.
type topicData struct {
	Topic
	triggers []string
}

// RelatedItem 은 임베딩 테이블에서 벡터 유사도로 검색된 한 건입니다.
// (sensitive_issues / slang_terms / mim_terms 공용 결과 형태)
type RelatedItem struct {
	Source     string  `json:"source"` // 출처 테이블
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Category   string  `json:"category,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Similarity float64 `json:"-"` // 내부 랭킹용. 응답에는 노출하지 않는다.
}

// searchSpec 은 벡터 검색 대상 테이블의 컬럼 매핑입니다.
// 컬럼명·식은 모두 아래 상수에서만 오므로 SQL 조합은 안전합니다.
type searchSpec struct {
	source      string // 결과 Source 라벨(= 테이블명)
	table       string
	idCol       string
	titleExpr   string
	catExpr     string
	snippetExpr string
}

var (
	specIssues = searchSpec{"sensitive_issues", "sensitive_issues", "issue_id", "title", "COALESCE(category,'')", "COALESCE(NULLIF(new_description,''), description, '')"}
	specSlang  = searchSpec{"slang_terms", "slang_terms", "id", "expression", "COALESCE(nuance,'')", "COALESCE(meaning,'')"}
	specMim    = searchSpec{"mim_terms", "mim_terms", "id", "COALESCE(word,'')", "''", "COALESCE(definition,'')"}
)

// store 는 DB(pgvector)에서 검색·조회를 담당합니다.
type store struct {
	pool pgxPool
}

// searchVector 는 pgvector 코사인 거리로 주제를 정렬해 상위 limit개 후보를 반환합니다.
// (embedding 컬럼 사용. 거리 연산자 <=> 는 코사인 거리, HNSW 인덱스 활용.)
// 반환 슬라이스는 벡터 유사도 내림차순이며, 융합 랭킹은 Go(fuseRank)에서 수행합니다.
func (s *store) searchVector(ctx context.Context, vec []float32, limit int) ([]topicData, error) {
	const q = `
		SELECT id, title, category, COALESCE(event_date, ''),
		       COALESCE(trigger_expressions, '[]'::jsonb), description,
		       1 - (embedding <=> $1::vector) AS similarity
		FROM sensitive_events
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2`
	rows, err := s.pool.Query(ctx, q, vectorLiteral(vec), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []topicData
	for rows.Next() {
		var t topicData
		var trigRaw []byte
		if err := rows.Scan(&t.ID, &t.Title, &t.Category, &t.EventDate, &trigRaw, &t.Description, &t.Similarity); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(trigRaw, &t.triggers) // jsonb 배열 -> []string
		out = append(out, t)
	}
	return out, rows.Err()
}

// searchSimilar 는 spec 테이블에서 pgvector 코사인 거리로 상위 limit개를 검색합니다.
// (embedding 컬럼 + HNSW 인덱스 사용. 모든 임베딩 테이블에 공통 적용.)
func (s *store) searchSimilar(ctx context.Context, spec searchSpec, vec []float32, limit int) ([]RelatedItem, error) {
	q := fmt.Sprintf(`
		SELECT %s::text, %s, %s, %s, 1 - (embedding <=> $1::vector) AS similarity
		FROM %s
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2`, spec.idCol, spec.titleExpr, spec.catExpr, spec.snippetExpr, spec.table)
	rows, err := s.pool.Query(ctx, q, vectorLiteral(vec), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RelatedItem
	for rows.Next() {
		it := RelatedItem{Source: spec.source}
		if err := rows.Scan(&it.ID, &it.Title, &it.Category, &it.Snippet, &it.Similarity); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// listEvents 는 민감 사건을 페이징 조회하고, 각 사건에 연결된 전례 건수와 전체 건수를 함께 반환합니다.
// (sensitive_events LEFT JOIN sensitive_issues 로 issue_count 집계.)
func (s *store) listEvents(ctx context.Context, limit, offset int) ([]EventListItem, int, error) {
	const countQ = `SELECT COUNT(*) FROM sensitive_events`
	var total int
	if err := s.pool.QueryRow(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("민감 사건 건수 조회 실패: %w", err)
	}

	const q = `
		SELECT e.id, e.title, COALESCE(e.category, ''), COALESCE(e.event_date, ''),
		       COUNT(i.issue_id) AS issue_count
		FROM sensitive_events e
		LEFT JOIN sensitive_issues i ON i.event_id = e.id
		GROUP BY e.id, e.title, e.category, e.event_date
		ORDER BY e.event_date DESC NULLS LAST, e.id
		LIMIT $1 OFFSET $2`
	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("민감 사건 목록 조회 실패: %w", err)
	}
	defer rows.Close()

	var out []EventListItem
	for rows.Next() {
		var it EventListItem
		var eventDate string
		if err := rows.Scan(&it.ID, &it.Title, &it.Category, &eventDate, &it.IssueCount); err != nil {
			return nil, 0, fmt.Errorf("민감 사건 행 스캔 실패: %w", err)
		}
		it.Year = yearFromDate(eventDate)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("민감 사건 순회 실패: %w", err)
	}
	return out, total, nil
}

// getEvent 는 사건 본체(전례 제외)를 조회합니다. 없으면 pgx.ErrNoRows 를 감싸 반환합니다.
func (s *store) getEvent(ctx context.Context, id string) (*EventDetail, error) {
	const q = `
		SELECT id, title, COALESCE(category, ''), COALESCE(event_date, ''), COALESCE(description, '')
		FROM sensitive_events
		WHERE id = $1`
	var d EventDetail
	var eventDate string
	if err := s.pool.QueryRow(ctx, q, id).Scan(&d.ID, &d.Title, &d.Category, &eventDate, &d.Description); err != nil {
		return nil, fmt.Errorf("민감 사건 조회 실패: %w", err)
	}
	d.Year = yearFromDate(eventDate)
	return &d, nil
}

// issuesForEvent 는 사건에 연결된 논란 전례를 FE 필드 형태로 매핑해 반환합니다.
// (brand←title, copy←description, year←issue_date. campaign/level/result 는 원천이 없어 빈 값.)
func (s *store) issuesForEvent(ctx context.Context, eventID string) ([]EventIssue, error) {
	const q = `
		SELECT issue_id, COALESCE(title, ''), COALESCE(description, ''), issue_date
		FROM sensitive_issues
		WHERE event_id = $1
		ORDER BY issue_id`
	rows, err := s.pool.Query(ctx, q, eventID)
	if err != nil {
		return nil, fmt.Errorf("연결 전례 조회 실패: %w", err)
	}
	defer rows.Close()

	var out []EventIssue
	for rows.Next() {
		var (
			it        EventIssue
			title     string
			desc      string
			issueDate *time.Time
		)
		if err := rows.Scan(&it.ID, &title, &desc, &issueDate); err != nil {
			return nil, fmt.Errorf("연결 전례 행 스캔 실패: %w", err)
		}
		it.Brand = title
		it.Copy = desc
		if issueDate != nil {
			it.Year = strconv.Itoa(issueDate.Year())
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("연결 전례 순회 실패: %w", err)
	}
	return out, nil
}

// vectorLiteral 는 []float32 를 pgvector 텍스트 리터럴("[0.1,0.2,...]")로 만듭니다.
// 쿼리에서 $1::vector 로 캐스팅해 사용합니다.
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
