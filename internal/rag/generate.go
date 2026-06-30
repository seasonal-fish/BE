package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// Trend 는 mim_terms(유행어/밈) 한 건의 활성도 정보입니다.
//   - Tag: '#'을 붙인 유행어(예: "#중꺾마")
//   - Category: mim_terms 에는 카테고리가 없어 항상 빈 문자열
//   - Definition: 신조어 설명(mim_terms.definition). 툴팁 표기용.
//   - Up: 활성도 점수(trend_score). 음수(하락세)일 수 있다.
//   - Rank: trend_score 내림차순 순위(1부터)
//   - Delta: 최근 7일 평균 검색비율 - 직전 7일 평균(search_ratios_90d 기반). 상승/하락 표기용.
//   - Ratios: 최근 90일 일별 검색비율(0~100, 단어별 자기 정규화). 활성도 추이 차트용.
type Trend struct {
	Tag        string  `json:"tag"`
	Category   string  `json:"category"`
	Definition string  `json:"definition"`
	Up         float64 `json:"up"`
	Rank       int     `json:"rank"`
	Delta      int     `json:"delta"`
	Ratios     []int   `json:"ratios"`
}

// trendingTerms 는 mim_terms 를 trend_score 내림차순으로 상위 limit 개 반환합니다.
// trend_score 를 활성도(Up)로, search_ratios_90d(최근 90일 일별 검색비율)를 추이(Ratios)로 노출하며,
// Delta 는 추이의 최근 7일 평균과 직전 7일 평균 차이로 산출합니다.
func (s *store) trendingTerms(ctx context.Context, limit int) ([]Trend, error) {
	const q = `
		SELECT word, definition, trend_score::float8, search_ratios_90d
		FROM mim_terms
		WHERE trend_score IS NOT NULL
		ORDER BY trend_score DESC, word ASC
		LIMIT $1`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("트렌드어 조회 실패: %w", err)
	}
	defer rows.Close()

	var out []Trend
	for rows.Next() {
		var (
			word       string
			definition string
			score      float64
			ratios     []int32
		)
		if err := rows.Scan(&word, &definition, &score, &ratios); err != nil {
			return nil, fmt.Errorf("트렌드어 스캔 실패: %w", err)
		}
		if word == "" {
			continue
		}
		out = append(out, Trend{
			Tag:        "#" + word,
			Definition: definition,
			Up:         score,
			// 쿼리가 trend_score DESC 로 정렬되므로 누적 인덱스가 곧 순위(1부터)다.
			Rank:   len(out) + 1,
			Delta:  recentDelta(ratios),
			Ratios: int32sToInts(ratios),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("트렌드어 행 순회 실패: %w", err)
	}
	return out, nil
}

// recentDelta 는 90일 추이에서 최근 7일 평균과 직전 7일 평균의 차이를 정수로 반환합니다.
// 표본이 14개 미만이면 마지막값-첫값(원소가 1개 이하이면 0)으로 대체합니다.
func recentDelta(ratios []int32) int {
	n := len(ratios)
	if n < 14 {
		if n >= 2 {
			return int(ratios[n-1]) - int(ratios[0])
		}
		return 0
	}
	var last, prev float64
	for _, v := range ratios[n-7:] {
		last += float64(v)
	}
	for _, v := range ratios[n-14 : n-7] {
		prev += float64(v)
	}
	return int(math.Round((last - prev) / 7.0))
}

// int32sToInts 는 pgx 가 정수배열을 스캔한 []int32 를 JSON/프론트에서 쓰기 좋은 []int 로 변환합니다.
func int32sToInts(in []int32) []int {
	if in == nil {
		return nil
	}
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

// Trends 는 상위 limit 개의 트렌드어를 반환합니다(store 위임).
func (s *Service) Trends(ctx context.Context, limit int) ([]Trend, error) {
	return s.store.trendingTerms(ctx, limit)
}

const generateSystem = `너는 한국의 광고 카피라이터다.
주어진 제품/캠페인 정보와 톤, 그리고 활용할 트렌드어를 바탕으로
매력적인 광고 헤드라인 후보 4개를 만든다.
모든 문구는 한국어로 작성한다.
반드시 다음 JSON 형식으로만 응답한다(설명·코드블록·여분 텍스트 금지):
{"candidates": ["문구1","문구2","문구3","문구4"]}`

// Generate 는 제품/톤/트렌드어로 광고 헤드라인 후보를 생성합니다.
// chat(jsonMode=true) 결과를 JSON 파싱해 []string 으로 반환합니다.
func (c *openAIClient) Generate(ctx context.Context, product, tone string, trends []string) ([]string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "제품/캠페인: %s\n", strings.TrimSpace(product))
	if strings.TrimSpace(tone) != "" {
		fmt.Fprintf(&b, "톤: %s\n", strings.TrimSpace(tone))
	}
	if len(trends) > 0 {
		fmt.Fprintf(&b, "활용할 트렌드어: %s\n", strings.Join(trends, ", "))
	}
	b.WriteString("위 정보를 활용해 광고 헤드라인 후보 4개를 만들어라.")

	raw, err := c.chat(ctx, generateSystem, b.String(), true)
	if err != nil {
		return nil, fmt.Errorf("문구 생성 호출 실패: %w", err)
	}

	var parsed struct {
		Candidates []string `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("문구 생성 응답 파싱 실패: %w", err)
	}
	if len(parsed.Candidates) == 0 {
		return nil, fmt.Errorf("문구 생성 응답에 후보가 없습니다")
	}
	return parsed.Candidates, nil
}

// GenerateRequest 는 /generate 요청 본문입니다.
type GenerateRequest struct {
	Product string   `json:"product"`
	Tone    string   `json:"tone"`
	Trends  []string `json:"trends"`
}

// GenerateCandidate 는 생성된 문구 한 건과 자동 검토 결과입니다.
type GenerateCandidate struct {
	Text        string `json:"text"`
	Score       int    `json:"score"`
	SafetyLabel string `json:"safety_label"` // 안전 | 주의 | 위험 | 검토실패
	Note        string `json:"note"`
	ReviewID    string `json:"review_id,omitempty"` // 검토 실패 시 비어 있으므로 생략
}

// Generate 는 후보 문구를 생성하고, 각 후보를 자동 리스크 검토한 결과를 반환합니다.
//   - ai.Generate 로 후보 텍스트 생성
//   - 각 후보를 Review 로 검토하고 SaveHistory 로 이력 저장(실패는 무시)
//   - Verdict.Score 로 안전 라벨 산출
func (s *Service) Generate(ctx context.Context, req GenerateRequest) ([]GenerateCandidate, error) {
	texts, err := s.ai.Generate(ctx, req.Product, req.Tone, req.Trends)
	if err != nil {
		return nil, fmt.Errorf("문구 생성 실패: %w", err)
	}

	// 후보들은 서로 독립적이므로 병렬로 검토한다(각 Review 는 Rewrite+Embed+Judge
	// 3회 OpenAI 왕복이라 직렬화하면 후보 수에 비례해 지연이 커진다).
	// 결과 순서를 보존하기 위해 인덱스 슬롯에 기록한다.
	slots := make([]GenerateCandidate, len(texts))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for i, text := range texts {
		i, text := i, strings.TrimSpace(text)
		if text == "" {
			continue
		}
		g.Go(func() error {
			// 후보 1건 검토가 실패해도 전체를 버리지 않고 해당 후보만 '검토실패'로
			// 표기해 부분 결과를 반환한다(에러를 errgroup 으로 전파하지 않음).
			start := time.Now()
			review, err := s.Review(gctx, text)
			if err != nil {
				slots[i] = GenerateCandidate{
					Text:        text,
					SafetyLabel: "검토실패",
					Note:        "리스크 검토 중 오류가 발생했습니다",
				}
				return nil
			}
			latencyMs := int(time.Since(start).Milliseconds())

			// 검토 이력 저장(베스트에포트). 요청 컨텍스트가 취소돼도 저장되도록
			// review.go 와 동일하게 별도의 짧은 타임아웃 컨텍스트를 쓴다.
			saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.SaveHistory(saveCtx, review, "generate", latencyMs)
			cancel()

			slots[i] = GenerateCandidate{
				Text:        text,
				Score:       review.Verdict.Score,
				SafetyLabel: safetyLabel(review.Verdict.Score),
				Note:        safetyNote(review.Verdict),
				ReviewID:    review.ID,
			}
			return nil
		})
	}
	_ = g.Wait() // 모든 작업이 nil 을 반환하므로 에러 없음(부분 실패는 슬롯에 표기)

	// 빈 text 로 건너뛴 슬롯을 제외하고 순서대로 모은다.
	out := make([]GenerateCandidate, 0, len(slots))
	for _, c := range slots {
		if c.Text != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// safetyNote 는 판정의 조언 또는 첫 사유를 노트로 사용합니다(둘 다 없으면 기본 문구).
// 안전(score<34, 라벨 '안전')인 후보는 경고성 advice 를 숨겨 라벨과 노트가
// 어긋나지 않게 합니다("안전"인데 경고문이 붙는 모순 방지).
func safetyNote(v Verdict) string {
	if v.Score < 34 {
		return "민감 표현 없음"
	}
	if strings.TrimSpace(v.Advice) != "" {
		return v.Advice
	}
	if len(v.Reasons) > 0 && strings.TrimSpace(v.Reasons[0]) != "" {
		return v.Reasons[0]
	}
	return "민감 표현 없음"
}
