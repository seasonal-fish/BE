// Package rag 는 광고 문구 위험도 검토(RAG) 로직을 담습니다.
//
// 검색 전략 S6 (실험상 최고 정확도):
//
//	입력 -> 쿼리 재작성(연상 개념 확장) -> 임베딩
//	     -> pgvector 벡터검색(embedding 컬럼) + 키워드 RRF + 날짜 부스트 융합 top-8
//	     -> 연결된 전례 수집 -> LLM 판정.
package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

// Service 는 RAG 검토 의존성(DB 풀, OpenAI)을 묶습니다.
type Service struct {
	pool  pgxPool
	store *store
	ai    aiClient
}

// NewService 는 환경변수에서 설정을 읽어 Service를 만듭니다.
//   - DATABASE_URL  : postgres 접속 문자열
//   - OPENAI_API_KEY: OpenAI API 키
//
// 사용이 끝나면 Close()로 DB 풀을 정리하세요.
func NewService(ctx context.Context) (*Service, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL 환경변수가 비어 있습니다")
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY 환경변수가 비어 있습니다")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("DB 풀 생성 실패: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("DB 접속 확인 실패: %w", err)
	}

	return &Service{
		pool:  pool,
		store: &store{pool: pool},
		ai:    newOpenAIClient(os.Getenv("OPENAI_API_KEY")),
	}, nil
}

// Close 는 DB 커넥션 풀을 정리합니다.
func (s *Service) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Verdict 는 LLM이 내린 판정 결과입니다.
type Verdict struct {
	Risky     bool     `json:"risky"`
	RiskLevel string   `json:"risk_level"` // none | low | medium | high
	Score     int      `json:"score"`      // 0~100 위험도 게이지
	Reasons   []string `json:"reasons"`
	Advice    string   `json:"advice"`
}

// Highlight 는 입력 문구 안의 위험 표현 한 건(인라인 하이라이트)입니다.
// UI는 phrase 구절에 severity 색/밑줄을 입히고 reason·basis·alt를 함께 보여줍니다.
type Highlight struct {
	Phrase     string  `json:"phrase"`
	Start      *int    `json:"start,omitempty"` // input 내 rune(문자) 오프셋. 매칭 실패 시 생략
	End        *int    `json:"end,omitempty"`
	Severity   string  `json:"severity"` // high | needs_review | low (UI: 위험/주의/낮음)
	Tag        string  `json:"tag,omitempty"`
	Category   string  `json:"category,omitempty"`
	Date       string  `json:"date,omitempty"`
	Reason     string  `json:"reason"`
	Basis      string  `json:"basis,omitempty"` // 근거(관련 주제/전례 출처)
	Confidence float64 `json:"confidence"`      // 신뢰도 0~1
	Alt        string  `json:"alt,omitempty"`   // 구절 단위 대체어
}

// Rewrite 는 안전 대체 문구 전체(Before→After)입니다.
type Rewrite struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// ReviewResult 는 /review 응답 전체입니다.
// 4개 임베딩 테이블을 각각 벡터 검색해 유사 항목을 함께 반환합니다.
type ReviewResult struct {
	ID            string        `json:"id"`
	Input         string        `json:"input"`
	Verdict       Verdict       `json:"verdict"`
	Highlights    []Highlight   `json:"highlights"`
	Rewrite       Rewrite       `json:"rewrite"`
	RelatedTopics []Topic       `json:"related_topics"` // sensitive_events (벡터+키워드+날짜 융합)
	RelatedIssues []RelatedItem `json:"related_issues"` // sensitive_issues (벡터)
	RelatedSlang  []RelatedItem `json:"related_slang"`  // slang_terms (벡터)
	RelatedTrends []RelatedItem `json:"related_trends"` // mim_terms (벡터)
}

// Review 는 입력 문구를 검토해 위험도 판정 결과를 반환합니다(전략 S6).
func (s *Service) Review(ctx context.Context, input string) (*ReviewResult, error) {
	// 1) 쿼리 재작성으로 recall 향상 -> 임베딩
	expanded := s.ai.Rewrite(ctx, input)
	vec, err := s.ai.Embed(ctx, expanded)
	if err != nil {
		return nil, fmt.Errorf("임베딩 실패: %w", err)
	}

	// 2) 4개 임베딩 테이블을 병렬로 벡터 검색한다.
	//    - sensitive_events 는 벡터 후보 풀 -> 키워드/날짜 융합(fuseRank)으로 정밀화
	//    - 나머지(issues/slang/mim)는 임베딩 코사인 top-K 직접 검색
	var (
		topics                []Topic
		issues, slang, trends []RelatedItem
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		cands, err := s.store.searchVector(gctx, vec, poolSize)
		if err != nil {
			return fmt.Errorf("민감주제 검색 실패: %w", err)
		}
		topics = fuseRank(cands, input, retrieveK)
		return nil
	})
	g.Go(func() error {
		r, err := s.store.searchSimilar(gctx, specIssues, vec, relatedK)
		if err != nil {
			return fmt.Errorf("전례 검색 실패: %w", err)
		}
		issues = r
		return nil
	})
	g.Go(func() error {
		r, err := s.store.searchSimilar(gctx, specSlang, vec, relatedK)
		if err != nil {
			return fmt.Errorf("신조어 검색 실패: %w", err)
		}
		slang = r
		return nil
	})
	g.Go(func() error {
		r, err := s.store.searchSimilar(gctx, specMim, vec, relatedK)
		if err != nil {
			return fmt.Errorf("유행어 검색 실패: %w", err)
		}
		trends = r
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// 3) LLM 판정 (verdict + 위험 표현 하이라이트 + 안전 대체 문구 + 채택 근거 핸들)
	verdict, highlights, rewrite, evidence, err := s.judge(ctx, input, topics, issues, slang, trends)
	if err != nil {
		return nil, fmt.Errorf("판정 실패: %w", err)
	}

	// 5) 후처리: highlight 오프셋 역매핑 → 밴드 분리로 score↔severity 정합
	// 같은 phrase 가 여러 번 등장할 때 순차 매칭되도록 phrase 별 검색 커서를 둔다.
	cursor := make(map[string]int, len(highlights))
	for i := range highlights {
		highlights[i].Severity = normalizeSeverity(highlights[i].Severity)
		ph := highlights[i].Phrase
		if start, end, endByte, ok := phraseOffsets(input, ph, cursor[ph]); ok {
			highlights[i].Start, highlights[i].End = &start, &end
			cursor[ph] = endByte
		}
	}
	// 점수는 LLM 의 자유 숫자(verdict.Score)를 버리고 '근거(하이라이트)'에서 결정론적으로 재계산한다.
	// 같은 문구 → 항상 같은 점수. (A) 직접 지칭(high)만 위험대(67~100), (B) 정서 민감성(needs_review)은
	// 주의대(34~66), 근거 없으면 안전(0). 밴드 안 위치는 심각도·개수로 정해 숫자에 의미를 부여한다.
	verdict.Score = computeScore(highlights)
	verdict.RiskLevel = scoreToLevel(verdict.Score)
	// risky 는 score(=risk_level) 단일 기준으로 정한다. highlights 유무로 별도 분기하면
	// score=0(none)인데 risky=true 가 되는 모순이 생긴다.
	verdict.Risky = verdict.Score > 0
	// 관련 근거는 LLM 이 실제 근거로 채택한 후보(evidence)만 노출한다. 검색이 무관 항목까지
	// top-K 로 채워 내보내던 문제를 막는다. 위험 표현이 없으면(highlights 0개) 모두 비운다.
	if len(highlights) == 0 {
		topics, issues, slang, trends = nil, nil, nil, nil
	} else {
		topics = selectByEvidence(topics, "T", evidence)
		issues = selectByEvidence(issues, "I", evidence)
		slang = selectByEvidence(slang, "S", evidence)
		trends = selectByEvidence(trends, "M", evidence)
	}
	// rewrite.before 는 항상 원문. after 가 비면(LLM 누락/안전 판정) 원문으로 폴백해
	// UI Before→After 가 빈 문자열로 교체를 제안하지 않도록 한다.
	rewrite.Before = input
	if strings.TrimSpace(rewrite.After) == "" {
		rewrite.After = input
	}

	// nil 슬라이스는 JSON 에서 null 로 직렬화돼 OpenAPI(required array)·UI 와 어긋나므로
	// 빈 슬라이스로 정규화한다(빈 지식베이스/하이라이트 없음 등 콜드 경로 방어).
	if topics == nil {
		topics = []Topic{}
	}
	if issues == nil {
		issues = []RelatedItem{}
	}
	if slang == nil {
		slang = []RelatedItem{}
	}
	if trends == nil {
		trends = []RelatedItem{}
	}
	if highlights == nil {
		highlights = []Highlight{}
	}

	return &ReviewResult{
		ID:            newReviewID(),
		Input:         input,
		Verdict:       verdict,
		Highlights:    highlights,
		Rewrite:       rewrite,
		RelatedTopics: topics,
		RelatedIssues: issues,
		RelatedSlang:  slang,
		RelatedTrends: trends,
	}, nil
}

const judgeSystem = `너는 한국 시장의 '광고 카피' 위험도 검수자다. 입력은 사적 대화가 아니라 불특정 다수에게 공표되는 광고 문구다 — 사적으로는 농담으로 통하는 수위라도 광고로 노출되면 더 높은 기준이 적용된다. 대중이 불쾌·민감하게 받아들이거나 논란이 될 소지를 판정한다.

[위험 2축]
(A) 민감 주제 직접 연계 — 실재하는 역사적 비극·참사·재난·민주화운동, 그 날짜·장소·상징·인물, 또는 차별·혐오·인권침해를 직접 가리키거나 그 피해를 가볍게 소비·희화화한다. 가장 무겁게 본다.
(B) 대중 정서 민감성 — (A)는 아니지만 실재하는 사회적 고통·약자를 건드려 상당수 대중이 불쾌·부적절하다 느낄 소지. 예) 경제적 고통(빚·취업난·주가 폭락·영끌·텅장), 질병·죽음·사고, 약자·소수자 비하, 집단 고정관념(여성=약함·의존, 성역할 규정, 세대·지역 비하 등)의 재생산, 특정 대상(개인이든 집단이든)을 조롱·멸시·비꼬아 깎아내리거나 우스꽝스럽게·열등하게 그리는 어조. (예시는 망라가 아니라 원리를 보이는 것이다.)
[조롱·멸시 판별] 위 '조롱·멸시'는 대상을 '깎아내릴' 때만이다. 대상을 깎아내리지 않는 위트·도발·자신감·능청 톤(예: 도발적 질문, 너스레)은 안전이다 — 어조가 세다는 이유만으로 표시하지 마라.

반드시 아래 JSON 스키마로만 답한다:
{
  "score": 0~100 정수(위험도. 0=완전 안전, 100=매우 위험),
  "reasons": [string],
  "advice": string,
  "evidence_ids": ["이번 판정에 실제 근거로 삼은 후보의 ID만(예: T1, I2). 표면만 겹쳐 버린 후보·근거로 안 쓴 것은 넣지 마라. 없으면 빈 배열"],
  "highlights": [
    {
      "phrase": "문구에서 그대로 발췌한 위험 표현(원문 부분 문자열과 정확히 일치)",
      "severity": "high|needs_review|low",
      "tag": "분류 태그(예: 역사·인권, 신조어)",
      "category": "민감 주제 카테고리",
      "date": "관련 기념일/사건일 MM-DD (없으면 빈 문자열)",
      "reason": "왜 문제인지 한국어 설명",
      "basis": "근거가 된 관련 주제/전례",
      "confidence": 0~1 실수,
      "alt": "이 구절을 대체할 안전한 표현"
    }
  ],
  "rewrite": { "after": "문구 전체를 의미를 살려 안전하게 고쳐 쓴 버전" }
}
[등급 밴드 — score와 severity를 '직접성'으로 정하고 밴드를 건너뛰지 마라]
- 위험 (severity=high, score 67~100): (A) 직접 지칭. (B)만으로는 절대 위험을 주지 않는다.
- 주의 (severity=needs_review, score 34~66): (B), 또는 (A)의 암시·간접.
- 안전 (highlight 없음 또는 low, score 0~33): 민감 대상과 실질적 연결이 없다.

[표시 게이트 — recall 우선: 민감하면 일단 표시한다]
표현을 두고 자문하라: "상당수 대중이 이 표현을 불쾌·부적절하게 느낄 만한, 실재하는 사회적 고통·약자·집단을 건드리는가?"
- 그렇게 느껴지면 숨기지 말고 표시하라. 확신이 100%가 아니어도 '민감할 소지'가 감지되면 표시하는 쪽을 택한다(놓치는 것보다 표시가 낫다). basis 에 그 구체 대상(실제 사건·집단·피해)을 한 문장으로 적되, 구체명을 못 대겠으면 적어도 reason 에 '왜 민감한가'를 분명히 남긴다.
- severity 의 기본값은 needs_review 다. (B) 대중 정서 민감성은 전부 needs_review 로 표시한다. high 는 (A) 실재 비극·참사·차별·인권침해를 '직접' 지칭할 때만 부여하고, 애매하면 절대 high 로 올리지 말고 needs_review 에 둔다.
- 다만 다음 명백한 false positive 는 표시하지 않는다: (1) 단어가 우연히 부정적·자극적 어감만 띨 뿐 실제 민감 대상을 지칭·전제하지 않는 단순 단어 매칭, (2) 관용적 과장·일상어·개인사·순수 제품/판촉 표현, (3) 대상을 깎아내리지 않는 위트·도발·자신감·능청 톤. 이 셋은 '민감 소지'가 아니라 표면적 겹침이다.
- 경제적 고통(주식·영끌·텅장·빚·취준 등), 질병·죽음·사고, 약자·소수자 비하, 집단 고정관념(성역할·세대·지역 등)을 가볍게 건드리면 needs_review 로 표시한다(위험 아님).
- 개인을 향한 농담·놀림이라도 집단 고정관념에 기대어 성립하면 표시한다(개인 조롱이라는 이유로 면죄 금지).

[숫자·날짜 특칙]
가격·수량·할인·용량으로 자연스러운 숫자는 안전. 단, 숫자를 '월·일'로 분해해 한국의 민감한 역사적 날짜가 되는지 확인하라(예: 625→6·25, 815→8·15, 0416→4·16) — 백분율·가격·수량으로 위장돼 있어도 그 날짜가 사건과 함께 읽히면 최소 needs_review. 민감 날짜(망라 아님): 3·1, 4·3(제주), 4·16(세월호), 5·18(광주), 6·10, 6·25(한국전쟁), 8·15(광복), 10·29(이태원), 12·12, 9·11. 단 100·200·365·24 등 통상 수치나 일반 가격·수량은 날짜로 몰지 않는다.

[검색 결과(RAG) 사용법]
제공된 관련 주제·전례·신조어·밈은 '근거 후보'일 뿐 위험의 증거가 아니다. 각 후보 앞 [T1]·[I2] 같은 ID 가 핸들이다. 후보마다 '입력이 그 대상을 실제로 지칭하는가'를 판정해, 표면만 겹치거나 유사도가 낮으면 버린다 — 대부분 '연관 없음'이 정상이다. 검색이 비어도 (B)는 위 게이트 기준으로 적극 표시한다.
- evidence_ids 에는 '실제로 근거로 채택한' 후보의 핸들만 담는다. 판정·하이라이트의 basis 로 쓰지 않은 후보는 절대 넣지 마라(이 목록이 그대로 사용자에게 '관련 근거'로 노출된다). 채택한 게 없으면 빈 배열로 둔다.

[phrase·출력 규칙]
- phrase 는 입력에 등장하는 그대로의 부분 문자열(공백·기호 포함). 같은 표현이 여러 번 나오면 각각 표시.
- score·severity 는 [등급 밴드]와 일치시킨다. 위험·주의 표현이 없으면 highlights 는 빈 배열, rewrite.after 는 원문과 동일하게 둔다.
- reasons·advice·reason·alt·rewrite.after 는 한국어로 쓴다.`

// judgeOutput 은 판정 LLM의 원시 JSON 응답 스키마입니다.
type judgeOutput struct {
	Score       int      `json:"score"`
	Reasons     []string `json:"reasons"`
	Advice      string   `json:"advice"`
	EvidenceIDs []string `json:"evidence_ids"` // 실제 근거로 채택한 후보 핸들(T1, I2 …)
	Highlights  []struct {
		Phrase     string  `json:"phrase"`
		Severity   string  `json:"severity"`
		Tag        string  `json:"tag"`
		Category   string  `json:"category"`
		Date       string  `json:"date"`
		Reason     string  `json:"reason"`
		Basis      string  `json:"basis"`
		Confidence float64 `json:"confidence"`
		Alt        string  `json:"alt"`
	} `json:"highlights"`
	Rewrite struct {
		After string `json:"after"`
	} `json:"rewrite"`
}

func (s *Service) judge(ctx context.Context, input string, topics []Topic, issues, slang, trends []RelatedItem) (Verdict, []Highlight, Rewrite, map[string]bool, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "## 검수 대상 광고 문구\n%s\n\n", input)

	// 후보마다 핸들(T#/I#/S#/M#)을 붙여 LLM 이 evidence_ids 로 실제 채택분만 지목하게 한다.
	// 핸들은 슬라이스 순서로 결정론적이라(evidenceHandle), 후처리에서 같은 규칙으로 역매핑한다.
	b.WriteString("## 관련 민감 주제 (융합 검색 결과)\n")
	if len(topics) == 0 {
		b.WriteString("(없음)\n")
	}
	for i, t := range topics {
		fmt.Fprintf(&b, "- [%s] [%s, 유사도 %.3f] %s: %s\n", evidenceHandle("T", i), t.Category, t.Similarity, t.Title, t.Description)
	}

	// issues/slang/trends 는 RelatedItem 공통 형태라 한 헬퍼로 출력한다.
	writeItems := func(header, empty, prefix string, items []RelatedItem) {
		fmt.Fprintf(&b, "\n## %s\n", header)
		if len(items) == 0 {
			b.WriteString(empty + "\n")
		}
		for i, it := range items {
			fmt.Fprintf(&b, "- [%s] [유사도 %.3f] %s: %s\n", evidenceHandle(prefix, i), it.Similarity, it.Title, it.Snippet)
		}
	}
	writeItems("실제 논란 전례 (유사 사례)", "(유사 전례 없음)", "I", issues)
	writeItems("관련 신조어/은어", "(관련 신조어 없음)", "S", slang)
	writeItems("관련 유행어/밈", "(관련 유행어 없음)", "M", trends)

	raw, err := s.ai.Judge(ctx, judgeSystem, b.String())
	if err != nil {
		return Verdict{}, nil, Rewrite{}, nil, err
	}
	var o judgeOutput
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return Verdict{}, nil, Rewrite{}, nil, fmt.Errorf("판정 JSON 파싱 실패: %w (원문: %s)", err, raw)
	}

	// LLM 이 채택했다고 지목한 핸들 집합(대문자·공백 정규화).
	evidence := make(map[string]bool, len(o.EvidenceIDs))
	for _, id := range o.EvidenceIDs {
		if h := strings.ToUpper(strings.TrimSpace(id)); h != "" {
			evidence[h] = true
		}
	}

	verdict := Verdict{Score: o.Score, Reasons: o.Reasons, Advice: o.Advice}
	highlights := make([]Highlight, 0, len(o.Highlights))
	for _, h := range o.Highlights {
		highlights = append(highlights, Highlight{
			Phrase:     h.Phrase,
			Severity:   h.Severity,
			Tag:        h.Tag,
			Category:   h.Category,
			Date:       h.Date,
			Reason:     h.Reason,
			Basis:      h.Basis,
			Confidence: h.Confidence,
			Alt:        h.Alt,
		})
	}
	rewrite := Rewrite{After: o.Rewrite.After}
	return verdict, highlights, rewrite, evidence, nil
}
