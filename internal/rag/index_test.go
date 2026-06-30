package rag

import (
	"reflect"
	"testing"
)

func TestDateSet(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  map[string]bool
	}{
		{
			name:  "한글 월일 표기",
			query: "8월 15일 광복절 기념 세일",
			want:  map[string]bool{"08-15": true},
		},
		{
			name:  "구분자 표기",
			query: "3.1 운동 기념",
			want:  map[string]bool{"03-01": true},
		},
		{
			name:  "여러 날짜 혼합",
			query: "5월 18일 그리고 4.16 추모",
			want:  map[string]bool{"05-18": true, "04-16": true},
		},
		{
			name:  "범위 밖 날짜는 제외",
			query: "13월 40일",
			want:  map[string]bool{},
		},
		{
			name:  "날짜 없음",
			query: "그냥 평범한 광고 문구",
			want:  map[string]bool{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dateSet(tt.query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dateSet(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestLexScore(t *testing.T) {
	tests := []struct {
		name     string
		triggers []string
		title    string
		query    string
		want     float64
	}{
		{
			name:     "구문 전체 일치는 2점",
			triggers: []string{"세월호"},
			title:    "다른제목",
			query:    "세월호 관련 문구",
			want:     2,
		},
		{
			name:     "제목 일치도 점수에 포함",
			triggers: []string{},
			title:    "광복절",
			query:    "광복절 세일",
			want:     2,
		},
		{
			name:     "단어 단위 부분 일치는 1점",
			triggers: []string{"민감 주제"},
			title:    "없는제목",
			query:    "이것은 민감 합니다",
			want:     1,
		},
		{
			name:     "빈 트리거는 무시",
			triggers: []string{"", "세월호"},
			title:    "",
			query:    "세월호",
			want:     2,
		},
		{
			name:     "일치 없으면 0점",
			triggers: []string{"전혀없음"},
			title:    "관계없음",
			query:    "무관한 문구입니다",
			want:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lexScore(tt.triggers, tt.title, tt.query)
			if got != tt.want {
				t.Errorf("lexScore(%v, %q, %q) = %v, want %v", tt.triggers, tt.title, tt.query, got, tt.want)
			}
		})
	}
}

func TestArgsortDesc(t *testing.T) {
	t.Run("primary 내림차순 정렬", func(t *testing.T) {
		primary := []float64{1, 3, 2}
		secondary := []float64{0, 0, 0}
		got := argsortDesc(primary, secondary)
		want := []int{1, 2, 0}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("argsortDesc = %v, want %v", got, want)
		}
	})

	t.Run("동점은 secondary 내림차순", func(t *testing.T) {
		primary := []float64{2, 2, 2}
		secondary := []float64{1, 3, 2}
		got := argsortDesc(primary, secondary)
		want := []int{1, 2, 0}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("argsortDesc = %v, want %v", got, want)
		}
	})

	t.Run("빈 입력", func(t *testing.T) {
		got := argsortDesc([]float64{}, []float64{})
		if len(got) != 0 {
			t.Errorf("argsortDesc(empty) = %v, want empty", got)
		}
	})
}

func TestRanks(t *testing.T) {
	t.Run("순서를 순위 역매핑으로 변환", func(t *testing.T) {
		// order[rank] = index 이므로 ranks[index] = rank
		order := []int{2, 0, 1}
		got := ranks(order)
		want := []int{1, 2, 0}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ranks(%v) = %v, want %v", order, got, want)
		}
	})

	t.Run("항등 순서", func(t *testing.T) {
		order := []int{0, 1, 2}
		got := ranks(order)
		want := []int{0, 1, 2}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ranks(%v) = %v, want %v", order, got, want)
		}
	})
}

func TestFuseRank(t *testing.T) {
	mk := func(id, title, date string, sim float64, triggers []string) topicData {
		return topicData{
			Topic: Topic{
				ID:         id,
				Title:      title,
				EventDate:  date,
				Similarity: sim,
			},
			triggers: triggers,
		}
	}

	t.Run("빈 후보는 nil 반환", func(t *testing.T) {
		if got := fuseRank(nil, "쿼리", 8); got != nil {
			t.Errorf("fuseRank(nil) = %v, want nil", got)
		}
	})

	t.Run("k가 후보 수보다 크면 전부 반환", func(t *testing.T) {
		cands := []topicData{
			mk("a", "제목A", "", 0.9, nil),
			mk("b", "제목B", "", 0.8, nil),
		}
		got := fuseRank(cands, "무관", 8)
		if len(got) != 2 {
			t.Fatalf("len(fuseRank) = %d, want 2", len(got))
		}
	})

	t.Run("상위 k개만 반환", func(t *testing.T) {
		cands := []topicData{
			mk("a", "제목A", "", 0.9, nil),
			mk("b", "제목B", "", 0.8, nil),
			mk("c", "제목C", "", 0.7, nil),
		}
		got := fuseRank(cands, "무관", 2)
		if len(got) != 2 {
			t.Fatalf("len(fuseRank) = %d, want 2", len(got))
		}
	})

	t.Run("날짜 일치 주제가 강하게 부스트됨", func(t *testing.T) {
		// b는 벡터 유사도가 낮지만 날짜가 쿼리와 일치 -> 최상위로 올라와야 함
		cands := []topicData{
			mk("a", "관련없는제목", "01-01", 0.95, nil),
			mk("b", "관련없는제목", "08-15", 0.10, nil),
		}
		got := fuseRank(cands, "8월 15일 행사", 2)
		if len(got) == 0 || got[0].ID != "b" {
			t.Errorf("날짜 부스트 실패: got[0]=%+v, want ID=b", got)
		}
	})

	t.Run("키워드 일치가 낮은 벡터 후보를 top-k로 끌어올림", func(t *testing.T) {
		// b는 벡터 유사도가 가장 낮지만 키워드가 원문과 일치 ->
		// 벡터만 보면 탈락할 c/d 를 제치고 top-2 안에 들어와야 함(전략 S6).
		cands := []topicData{
			mk("a", "최상위주제", "", 0.90, nil),
			mk("c", "중간주제", "", 0.30, nil),
			mk("d", "중간주제", "", 0.30, nil),
			mk("b", "특정키워드", "", 0.20, []string{"특정키워드"}),
		}
		got := fuseRank(cands, "특정키워드 포함 문구", 2)
		ids := make(map[string]bool, len(got))
		for _, t := range got {
			ids[t.ID] = true
		}
		if !ids["b"] {
			t.Errorf("키워드 부스트 실패: b가 top-2에 없음, got=%+v", got)
		}
		if ids["c"] || ids["d"] {
			t.Errorf("키워드 부스트 실패: 더 높은 벡터 후보 c/d가 b보다 앞섬, got=%+v", got)
		}
	})
}

func TestHasExactTriggerMatch(t *testing.T) {
	tests := []struct {
		name     string
		triggers []string
		query    string
		want     bool
	}{
		{
			name:     "3글자 이상 트리거가 원문에 그대로 등장",
			triggers: []string{"박종철"},
			query:    "오늘 박종철 열사 추모식",
			want:     true,
		},
		{
			name:     "트리거가 원문에 없으면 false",
			triggers: []string{"박종철"},
			query:    "평범한 여름 세일 문구",
			want:     false,
		},
		{
			name:     "2글자 트리거는 길이 게이트(3 rune)로 무시",
			triggers: []string{"세월"},
			query:    "세월 가는 줄 모른다",
			want:     false,
		},
		{
			name:     "3글자(rune) 경계 트리거는 매칭",
			triggers: []string{"123"},
			query:    "abc123def",
			want:     true,
		},
		{
			name:     "앞뒤 공백은 트리밍 후 매칭",
			triggers: []string{"  책상을 탁  "},
			query:    "이 여름, 책상을 탁 치고 떠나는 특가",
			want:     true,
		},
		{
			name:     "여러 트리거 중 하나라도 매칭되면 true",
			triggers: []string{"없는문구", "박종철"},
			query:    "박종철 관련 추모",
			want:     true,
		},
		{
			name:     "빈 슬라이스는 false",
			triggers: nil,
			query:    "아무 문구",
			want:     false,
		},
		{
			name:     "공백뿐인 트리거는 트리밍 후 길이 0이라 무시",
			triggers: []string{"   "},
			query:    "   ",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasExactTriggerMatch(tt.triggers, tt.query); got != tt.want {
				t.Errorf("hasExactTriggerMatch(%v, %q) = %v, want %v", tt.triggers, tt.query, got, tt.want)
			}
		})
	}
}
