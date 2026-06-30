package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubOpenAI 는 주어진 핸들러로 응답하는 가짜 OpenAI 서버에 붙은 클라이언트를 만든다.
func stubOpenAI(t *testing.T, handler http.HandlerFunc) *openAIClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &openAIClient{apiKey: "test-key", base: srv.URL, http: srv.Client()}
}

func embeddingResponse(vecs ...[]float32) string {
	type item struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	out := struct {
		Data []item `json:"data"`
	}{}
	for i, v := range vecs {
		out.Data = append(out.Data, item{Index: i, Embedding: v})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func chatResponse(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"content": content}}},
	})
	return string(b)
}

func TestEmbedBatch_OK(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		w.Write([]byte(embeddingResponse([]float32{0.1, 0.2}, []float32{0.3, 0.4})))
	})
	vecs, err := c.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 2 || vecs[1][0] != 0.3 {
		t.Fatalf("unexpected vecs: %v", vecs)
	}
}

func TestEmbedBatch_CountMismatch(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(embeddingResponse([]float32{0.1}))) // 1개만 반환
	})
	if _, err := c.EmbedBatch(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("개수 불일치인데 에러가 없음")
	}
}

func TestEmbedBatch_HTTPError(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	})
	if _, err := c.EmbedBatch(context.Background(), []string{"a"}); err == nil {
		t.Fatal("500 응답인데 에러가 없음")
	}
}

func TestEmbed_Single(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(embeddingResponse([]float32{1, 2, 3})))
	})
	v, err := c.Embed(context.Background(), "hello")
	if err != nil || len(v) != 3 {
		t.Fatalf("Embed: %v %v", v, err)
	}
}

func TestJudge_OK(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(chatResponse(`{"score":50}`)))
	})
	out, err := c.Judge(context.Background(), "sys", "user")
	if err != nil || !strings.Contains(out, "score") {
		t.Fatalf("Judge: %q %v", out, err)
	}
}

func TestChat_EmptyChoices(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	})
	if _, err := c.chat(context.Background(), "s", "u", true); err == nil {
		t.Fatal("빈 choices인데 에러가 없음")
	}
}

func TestGenerate_OK(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(chatResponse(`{"candidates":["문구1","문구2"]}`)))
	})
	got, err := c.Generate(context.Background(), "에코백", "위트있게", []string{"#여름"})
	if err != nil || len(got) != 2 {
		t.Fatalf("Generate: %v %v", got, err)
	}
}

func TestGenerate_EmptyCandidates(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(chatResponse(`{"candidates":[]}`)))
	})
	if _, err := c.Generate(context.Background(), "p", "", nil); err == nil {
		t.Fatal("후보 0개인데 에러가 없음")
	}
}

func TestRewrite_OK(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(chatResponse("광복절, 역사")))
	})
	got := c.Rewrite(context.Background(), "8월 세일")
	if !strings.Contains(got, "8월 세일") || !strings.Contains(got, "광복절") {
		t.Fatalf("Rewrite = %q", got)
	}
}

func TestRewrite_GracefulOnError(t *testing.T) {
	c := stubOpenAI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	// 실패해도 원문을 그대로 반환해야 한다(graceful).
	if got := c.Rewrite(context.Background(), "원문"); got != "원문" {
		t.Fatalf("Rewrite = %q, want 원문", got)
	}
}
