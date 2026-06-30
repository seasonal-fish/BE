package rag

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

// evidenceHandle 은 후보의 표시 순서(0부터)로 안정적 핸들(예: "T1", "I3")을 만듭니다.
// judge 프롬프트와 후처리 필터가 같은 규칙을 써야 역매핑이 맞습니다.
func evidenceHandle(prefix string, i int) string {
	return fmt.Sprintf("%s%d", prefix, i+1)
}

// selectByEvidence 는 LLM 이 채택한 핸들(picked)에 해당하는 후보만 순서대로 남깁니다.
// 핸들은 prefix + (인덱스+1) 로, judge 프롬프트에 부여한 것과 동일합니다.
func selectByEvidence[T any](items []T, prefix string, picked map[string]bool) []T {
	out := make([]T, 0, len(items))
	for i := range items {
		if picked[evidenceHandle(prefix, i)] {
			out = append(out, items[i])
		}
	}
	return out
}

// clampScore 는 위험도 점수를 0~100 범위로 보정합니다.
func clampScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

// scoreToLevel 은 0~100 점수를 risk_level 4단계로 매핑합니다.
// UI 게이지 눈금(33/66)에 맞춰 경계를 잡습니다.
func scoreToLevel(score int) string {
	switch {
	case score >= 67:
		return "high"
	case score >= 34:
		return "medium"
	case score >= 1:
		return "low"
	default:
		return "none"
	}
}

// safetyLabel 은 위험 점수(0~100)를 한국어 안전 라벨로 변환합니다.
// scoreToLevel 을 단일 진실원천으로 경유해 risk_level 과 항상 일치시킵니다.
func safetyLabel(score int) string {
	switch scoreToLevel(score) {
	case "high":
		return "위험"
	case "medium":
		return "주의"
	default: // low, none
		return "안전"
	}
}

// statusLabel 은 위험 점수로 히스토리 상태 라벨을 정합니다.
// medium 이상(score>=34)은 사람 검토가 필요한 needs_review, 그 외는 reviewed.
// scoreToLevel 을 경유해 safetyLabel·통계와 경계를 통일합니다.
func statusLabel(score int) string {
	switch scoreToLevel(score) {
	case "high", "medium":
		return "needs_review"
	default:
		return "reviewed"
	}
}

// normalizeSeverity 는 하이라이트 심각도를 UI 3단계(high|needs_review|low)로 정규화합니다.
// LLM이 medium/주의/경고 등 변형을 줘도 가장 가까운 단계로 흡수합니다.
func normalizeSeverity(sev string) string {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "high", "위험", "danger", "critical":
		return "high"
	case "low", "낮음", "minor":
		return "low"
	default:
		// medium, needs_review, 주의, warning 등은 모두 '주의'로 흡수
		return "needs_review"
	}
}

// severityScoreFloor 는 하이라이트 심각도 중 가장 높은 것에 대응하는 전체 점수의 하한을 돌려줍니다.
//   - 위험(high) 표현이 하나라도 있으면 67(위험 밴드 하한)
//   - 주의(needs_review) 표현이 있으면 34(주의 밴드 하한)
//   - 그 외 0
//
// LLM 이 전체 score 와 표현별 severity 를 따로 내며 "전체는 안전(score<34)인데 주의 표현 1건"
// 같은 모순이 생기는데, 후처리에서 score 를 이 하한까지 끌어올려 게이지와 칩을 항상 일치시킵니다.
// (입력은 normalizeSeverity 로 high|needs_review|low 로 정규화된 하이라이트여야 합니다.)
func severityScoreFloor(highlights []Highlight) int {
	floor := 0
	for _, h := range highlights {
		switch h.Severity {
		case "high":
			return 67
		case "needs_review":
			if floor < 34 {
				floor = 34
			}
		}
	}
	return floor
}

// clampBand 는 점수를 [lo,hi] 밴드 안으로 보정합니다.
func clampBand(score, lo, hi int) int {
	if score < lo {
		return lo
	}
	if score > hi {
		return hi
	}
	return score
}

// computeScore 는 하이라이트(근거)만으로 0~100 위험 점수를 결정론적으로 산출합니다.
// LLM 이 내는 자유 형식 score·confidence 는 쓰지 않는다 — 그 자체가 노이즈원이라, 같은 문구에도
// 점수가 흔들리는 원인이었다. 대신 '플래그된 표현의 심각도와 개수'라는 관측 가능한 근거만 쓴다.
// 따라서 같은 하이라이트 → 항상 같은 점수이며, 숫자가 곧 "어느 밴드 + 그 안에서 근거가 얼마나 누적됐나"를 뜻한다.
//
//	위험(high 존재)       : 75 기준, 추가 high 당 +8, 동반 needs_review 당 +3(최대 3건) → [67,100]
//	주의(needs_review 만)  : 45 기준, 추가 needs_review 당 +6                        → [34, 66]
//	낮음(low 만)          : 15 기준, 추가 low 당 +4                                 → [ 1, 33] (라벨은 안전)
//	근거 없음             : 0 (안전)
//
// (입력은 normalizeSeverity 로 high|needs_review|low 로 정규화된 하이라이트여야 합니다.)
func computeScore(highlights []Highlight) int {
	var nHigh, nNeed, nLow int
	for _, h := range highlights {
		switch h.Severity {
		case "high":
			nHigh++
		case "needs_review":
			nNeed++
		case "low":
			nLow++
		}
	}
	switch {
	case nHigh > 0:
		companion := nNeed
		if companion > 3 {
			companion = 3
		}
		return clampBand(75+8*(nHigh-1)+3*companion, 67, 100)
	case nNeed > 0:
		return clampBand(45+6*(nNeed-1), 34, 66)
	case nLow > 0:
		return clampBand(15+4*(nLow-1), 1, 33)
	default:
		return 0
	}
}

// phraseOffsets 는 input 의 fromByte 이후에서 phrase 를 찾아 rune(문자) 오프셋
// [start, end) 와 다음 검색 시작 byte 위치(matchEndByte)를 반환합니다.
// 찾지 못하면 ok=false. JS 측 인덱싱과 맞추기 위해 byte가 아닌 rune 단위로 반환합니다.
// fromByte 를 호출부에서 누적하면 같은 phrase 가 여러 번 등장할 때 순차 매칭됩니다.
func phraseOffsets(input, phrase string, fromByte int) (start, end, matchEndByte int, ok bool) {
	if phrase == "" || fromByte < 0 || fromByte > len(input) {
		return 0, 0, 0, false
	}
	rel := strings.Index(input[fromByte:], phrase)
	if rel < 0 {
		return 0, 0, 0, false
	}
	byteIdx := fromByte + rel
	start = utf8.RuneCountInString(input[:byteIdx])
	end = start + utf8.RuneCountInString(phrase)
	matchEndByte = byteIdx + len(phrase)
	return start, end, matchEndByte, true
}

// newReviewID 는 "rev_xxxx" 형태의 검토 ID를 생성합니다(히스토리 PK 겸용).
// 16바이트(128비트) 난수로 충돌 확률을 사실상 0으로 만듭니다.
func newReviewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "rev_00000000000000000000000000000000"
	}
	return "rev_" + hex.EncodeToString(b[:])
}
