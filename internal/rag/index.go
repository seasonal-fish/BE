package rag

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// 판정 LLM에 넘길 주제 개수(top-k). 실험상 Recall@8가 가장 안정적.
	retrieveK = 8
	// pgvector로 가져올 후보 풀 크기. 전체 주제 수(현재 96)보다 크게 잡아
	// 융합 랭킹이 누락 없이 동작하도록 함.
	poolSize = 200
	// relatedK 는 보조 임베딩 테이블(issues/slang/mim)에서 가져올 유사 항목 수.
	relatedK = 5
)

// fuseRank 는 pgvector가 벡터순으로 준 후보(cands)에 키워드(RRF)와 날짜 부스트를
// 결합해(전략 S6) 최종 상위 k개 주제를 반환합니다.
//
//   - 벡터 순위: cands의 슬라이스 위치(이미 코사인 거리순 정렬)
//   - 키워드 순위: 원문과 trigger_expressions/title 매칭 점수
//   - 날짜: 원문에서 추출한 MM-DD가 주제 event_date와 일치하면 강하게 부스트
func fuseRank(cands []topicData, origQuery string, k int) []Topic {
	n := len(cands)
	if n == 0 {
		return nil
	}
	dates := dateSet(origQuery)

	vsim := make([]float64, n)
	lex := make([]float64, n)
	for i := range cands {
		vsim[i] = cands[i].Similarity // pgvector 코사인 유사도
		lex[i] = lexScore(cands[i].triggers, cands[i].Title, origQuery)
	}

	// 벡터 순위는 입력 순서(이미 정렬됨). 키워드 순위는 점수 내림차순(동점은 벡터로).
	vrank := make([]int, n)
	for i := range vrank {
		vrank[i] = i
	}
	krank := ranks(argsortDesc(lex, vsim))

	final := make([]float64, n)
	for i := range cands {
		rrf := 1/float64(60+vrank[i]+1) + 1/float64(60+krank[i]+1)
		if dates[cands[i].EventDate] {
			rrf += 1.0 // 날짜 일치 강한 부스트
		}
		if hasExactTriggerMatch(cands[i].triggers, origQuery) {
			rrf += 1.0 // 트리거 표현이 원문에 그대로 등장하면 강한 부스트(아이코닉 문구 surfacing)
		}
		final[i] = rrf
	}

	if k > n {
		k = n
	}
	order := argsortDesc(final, vsim)
	out := make([]Topic, 0, k)
	for _, i := range order[:k] {
		out = append(out, cands[i].Topic) // Similarity는 이미 코사인 유사도
	}
	return out
}

// ---------- 헬퍼 ----------

// argsortDesc 는 primary 내림차순(동점은 secondary 내림차순)으로 인덱스를 정렬합니다.
func argsortDesc(primary, secondary []float64) []int {
	idx := make([]int, len(primary))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		if primary[ia] != primary[ib] {
			return primary[ia] > primary[ib]
		}
		return secondary[ia] > secondary[ib]
	})
	return idx
}

// ranks[i] = 인덱스 i의 순위(0부터).
func ranks(order []int) []int {
	r := make([]int, len(order))
	for rank, i := range order {
		r[i] = rank
	}
	return r
}

var dateMD = regexp.MustCompile(`(\d{1,2})\s*월\s*(\d{1,2})\s*일`)
var dateSep = regexp.MustCompile(`(\d{1,2})[./·](\d{1,2})`)

// dateSet 은 문구에서 날짜를 뽑아 "MM-DD" 집합으로 반환합니다.
func dateSet(q string) map[string]bool {
	out := map[string]bool{}
	add := func(mo, dy string) {
		m, _ := strconv.Atoi(mo)
		d, _ := strconv.Atoi(dy)
		if m >= 1 && m <= 12 && d >= 1 && d <= 31 {
			out[fmt.Sprintf("%02d-%02d", m, d)] = true
		}
	}
	for _, m := range dateMD.FindAllStringSubmatch(q, -1) {
		add(m[1], m[2])
	}
	for _, m := range dateSep.FindAllStringSubmatch(q, -1) {
		add(m[1], m[2])
	}
	return out
}

// hasExactTriggerMatch 는 trigger_expressions 중 하나라도 원문에 '그대로' 등장하는지 봅니다.
// 짧은 단어 오매치를 막기 위해 3글자(rune) 이상 트리거만 본다 — fuseRank 에서 강한 부스트의 트리거.
// 아이코닉 문구('책상을 탁', '박종철' 등)가 긴 광고 문장에 묻혀 벡터 유사도가 낮아도 상위에 올린다.
func hasExactTriggerMatch(triggers []string, q string) bool {
	for _, ph := range triggers {
		ph = strings.TrimSpace(ph)
		if utf8.RuneCountInString(ph) >= 3 && strings.Contains(q, ph) {
			return true
		}
	}
	return false
}

// lexScore 는 키워드/제목이 문구에 등장하는 정도를 점수화합니다.
func lexScore(triggers []string, title, q string) float64 {
	cand := append(append([]string{}, triggers...), title)
	var score float64
	for _, ph := range cand {
		if ph == "" {
			continue
		}
		if strings.Contains(q, ph) {
			score += 2
			continue
		}
		for _, w := range strings.Fields(ph) {
			if utf8.RuneCountInString(w) >= 2 && strings.Contains(q, w) {
				score += 1
				break
			}
		}
	}
	return score
}
