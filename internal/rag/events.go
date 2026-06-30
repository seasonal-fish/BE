package rag

import "context"

// EventListItem 은 민감 사건 목록(GET /events)의 한 행입니다.
//   - Year: event_date(텍스트)에서 추출한 연도
//   - IssueCount: 이 사건에 연결된 논란 전례(sensitive_issues) 건수
type EventListItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Category   string `json:"category"`
	Year       string `json:"year"`
	IssueCount int    `json:"issue_count"`
}

// EventDetail 은 민감 사건 상세(GET /events/:id)입니다.
// Issues 는 이 사건에 연결된 논란 전례 목록입니다.
type EventDetail struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Year        string       `json:"year"`
	Category    string       `json:"category"`
	Description string       `json:"description"`
	Issues      []EventIssue `json:"issues"`
}

// EventIssue 는 사건에 연결된 논란 전례 한 건입니다.
//
// FE가 기대하는 필드(brand/campaign/copy/level/result)에 맞춰 키를 노출하되,
// 운영 DB(sensitive_issues)에 구조화된 원천이 없는 값은 가진 데이터로 매핑하거나 빈 값으로 둡니다.
//   - Brand : title (브랜드명이 제목에 포함됨 — 가장 가까운 원천)
//   - Copy  : description (사건/광고 설명)
//   - Year  : issue_date 의 연도
//   - Campaign/Level/Result : 운영 DB에 원천이 없어 빈 값(FE에서 정합)
type EventIssue struct {
	ID       string `json:"id"`
	Brand    string `json:"brand"`
	Campaign string `json:"campaign"`
	Year     string `json:"year"`
	Level    string `json:"level"`
	Copy     string `json:"copy"`
	Result   string `json:"result"`
}

// ListEvents 는 민감 사건 목록을 페이징 조회하고 전체 건수도 반환합니다(store 위임).
func (s *Service) ListEvents(ctx context.Context, limit, offset int) ([]EventListItem, int, error) {
	return s.store.listEvents(ctx, limit, offset)
}

// GetEvent 는 사건 본체와 연결된 전례를 합쳐 상세를 반환합니다.
// 사건이 없으면 store.getEvent 가 pgx.ErrNoRows 를 감싸 반환하고, 핸들러가 404 로 매핑합니다.
func (s *Service) GetEvent(ctx context.Context, id string) (*EventDetail, error) {
	detail, err := s.store.getEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	issues, err := s.store.issuesForEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	if issues == nil {
		issues = []EventIssue{}
	}
	detail.Issues = issues
	return detail, nil
}

// yearFromDate 는 날짜 문자열에서 앞쪽의 연속된 숫자 4자리(연도)를 추출합니다.
// "2017-04-04", "1987", "1987년" 모두 "2017"/"1987" 을 반환하고, 없으면 빈 문자열을 반환합니다.
func yearFromDate(s string) string {
	start, n := -1, 0
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
			n++
			if n == 4 {
				return s[start : start+4]
			}
		} else {
			start, n = -1, 0
		}
	}
	return ""
}
