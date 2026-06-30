package rag

import (
	"reflect"
	"testing"
)

func TestSafetyNote(t *testing.T) {
	tests := []struct {
		name string
		v    Verdict
		want string
	}{
		{
			name: "안전(score<34)은 advice가 있어도 숨김",
			v:    Verdict{Score: 10, Advice: "이 표현은 위험합니다"},
			want: "민감 표현 없음",
		},
		{
			name: "주의 이상이고 advice 있으면 advice",
			v:    Verdict{Score: 50, Advice: "표현을 완화하세요"},
			want: "표현을 완화하세요",
		},
		{
			name: "advice 비고 reasons 있으면 첫 사유",
			v:    Verdict{Score: 80, Reasons: []string{"역사적 비극 연상", "추가 사유"}},
			want: "역사적 비극 연상",
		},
		{
			name: "advice 공백이면 reasons로 폴백",
			v:    Verdict{Score: 70, Advice: "   ", Reasons: []string{"사유1"}},
			want: "사유1",
		},
		{
			name: "주의 이상인데 advice·reasons 모두 없으면 기본 문구",
			v:    Verdict{Score: 70},
			want: "민감 표현 없음",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safetyNote(tt.v); got != tt.want {
				t.Errorf("safetyNote(%+v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestRecentDelta(t *testing.T) {
	tests := []struct {
		name   string
		ratios []int32
		want   int
	}{
		{
			name:   "14개 이상 상승: 최근7평균(10)-직전7평균(0)",
			ratios: []int32{0, 0, 0, 0, 0, 0, 0, 10, 10, 10, 10, 10, 10, 10},
			want:   10,
		},
		{
			name:   "14개 이상 하락: 음수 델타",
			ratios: []int32{10, 10, 10, 10, 10, 10, 10, 0, 0, 0, 0, 0, 0, 0},
			want:   -10,
		},
		{
			name:   "14개 이상 반올림: (4-0)/7=0.57→1",
			ratios: []int32{0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0},
			want:   1,
		},
		{
			name:   "14개 미만(2개 이상): 마지막-처음",
			ratios: []int32{5, 20},
			want:   15,
		},
		{
			name:   "원소 1개: 0",
			ratios: []int32{42},
			want:   0,
		},
		{
			name:   "빈 슬라이스: 0",
			ratios: nil,
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recentDelta(tt.ratios); got != tt.want {
				t.Errorf("recentDelta(%v) = %d, want %d", tt.ratios, got, tt.want)
			}
		})
	}
}

func TestInt32sToInts(t *testing.T) {
	// nil 입력은 nil 그대로(JSON 에서 null). 빈 슬라이스와 구분된다.
	if got := int32sToInts(nil); got != nil {
		t.Errorf("int32sToInts(nil) = %v, want nil", got)
	}
	// 빈(non-nil) 슬라이스는 빈 non-nil 슬라이스로 유지.
	if got := int32sToInts([]int32{}); got == nil || len(got) != 0 {
		t.Errorf("int32sToInts([]) = %v, want 빈 non-nil 슬라이스", got)
	}
	// 정상: 원소별 int 변환.
	if got := int32sToInts([]int32{1, 2, 3}); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("int32sToInts([1 2 3]) = %v, want [1 2 3]", got)
	}
}
