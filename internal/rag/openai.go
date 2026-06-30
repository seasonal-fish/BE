package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	embedModel = "text-embedding-3-small" // 1536차원
	openAIBase = "https://api.openai.com/v1"
)

// chatModel 은 판정/생성에 쓰는 채팅 모델(기본 gpt-5.4-mini). 환경변수 OPENAI_CHAT_MODEL 로 교체 가능.
// 예: OPENAI_CHAT_MODEL=gpt-4o-mini
var chatModel = func() string {
	if m := os.Getenv("OPENAI_CHAT_MODEL"); m != "" {
		return m
	}
	return "gpt-5.4-mini"
}()

// openAIClient 는 OpenAI 임베딩/채팅 API를 호출하는 얇은 래퍼입니다.
type openAIClient struct {
	apiKey string
	base   string // API 베이스 URL. 테스트에서 스텁 서버로 주입할 수 있도록 필드로 둔다.
	http   *http.Client
}

func newOpenAIClient(apiKey string) *openAIClient {
	return &openAIClient{
		apiKey: apiKey,
		base:   openAIBase,
		http:   &http.Client{Timeout: 30 * time.Second}, // 호출당 실측 ~2~4s(gpt-5.4-mini). 30s면 꼬리+여유 충분, 행은 빨리 실패
	}
}

func (c *openAIClient) post(ctx context.Context, path string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai %s: status %d: %s", path, resp.StatusCode, string(data))
	}
	return json.Unmarshal(data, out)
}

// EmbedBatch 는 여러 텍스트를 한 번의 요청으로 임베딩합니다.
func (c *openAIClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := c.post(ctx, "/embeddings", map[string]any{
		"model": embedModel,
		"input": texts,
	}, &out); err != nil {
		return nil, err
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("openai: embedding 개수 불일치 (%d != %d)", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("openai: embedding index 범위 초과 (%d, n=%d)", d.Index, len(texts))
		}
		vecs[d.Index] = d.Embedding // index 순서 보정
	}
	// 중복/누락 index 로 인해 비어 있는 슬롯이 없는지 확인(부분 nil 임베딩 방지)
	for i, v := range vecs {
		if len(v) == 0 {
			return nil, fmt.Errorf("openai: embedding 누락 (index %d)", i)
		}
	}
	return vecs, nil
}

// Embed 는 단일 텍스트를 임베딩합니다.
func (c *openAIClient) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// chat 은 chatModel(기본 gpt-5.4-mini)에 system/user 프롬프트를 보내고 content를 반환합니다.
// jsonMode=true면 JSON 객체 응답을 강제합니다.
func (c *openAIClient) chat(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	body := map[string]any{
		"model":       chatModel,
		"temperature": 0.2,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	if jsonMode {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := c.post(ctx, "/chat/completions", body, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: empty chat response")
	}
	return out.Choices[0].Message.Content, nil
}

const rewriteSystem = `너는 광고 검수 보조다. 입력 광고 문구가 연상시킬 수 있는
한국 사회의 민감한 사건·역사·기념일·인물·키워드를 쉼표로 5~8개만 나열한다.
설명, 문장, 줄바꿈 없이 키워드만 쉼표로 답한다.`

// Rewrite 는 입력 문구를 검색 recall을 높이기 위해 연상 개념으로 확장합니다.
// 원문 + 확장 키워드를 합쳐 반환합니다.
func (c *openAIClient) Rewrite(ctx context.Context, query string) string {
	expanded, err := c.chat(ctx, rewriteSystem, query, false)
	if err != nil || expanded == "" {
		return query // 실패해도 원문으로 진행(graceful)
	}
	return query + " " + expanded
}

// Judge 는 판정 프롬프트로 JSON 결과를 받아 그대로 반환합니다.
func (c *openAIClient) Judge(ctx context.Context, system, user string) (string, error) {
	return c.chat(ctx, system, user, true)
}
