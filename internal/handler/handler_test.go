package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/limbs713/BE/internal/image"
	"github.com/limbs713/BE/internal/rag"
	"github.com/limbs713/BE/internal/router"
)

// newTestRouter 는 의존성(DB/OpenAI) 없는 zero-value Service 로 라우터를 만든다.
// 아래 테스트들은 모두 핸들러가 svc 메서드를 호출하기 "전"(요청 검증·라우팅·CORS 단계)에서
// 끝나는 경로만 다루므로 nil 의존성으로도 안전하다.
func newTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return router.New(&rag.Service{}, &image.Service{})
}

func do(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 헬스 체크: 의존성 없이 200 + {"status":"ok"} (R-21 가용성 프로브)
func TestHealth_OK(t *testing.T) {
	w := do(t, newTestRouter(), http.MethodGet, "/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %s, want status ok", w.Body.String())
	}
}

// /review 입력 검증 (R-01): 잘못된 JSON·빈 텍스트·공백은 svc 호출 전에 400.
func TestReview_Validation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"invalid json", "{"},
		{"empty text", `{"text":""}`},
		{"blank text", `{"text":"   "}`},
		{"missing field", `{}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := do(t, newTestRouter(), http.MethodPost, "/review", c.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
		})
	}
}

// /generate 입력 검증: product 누락·공백은 400.
func TestGenerate_Validation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"invalid json", "{"},
		{"missing product", `{}`},
		{"blank product", `{"product":"   "}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := do(t, newTestRouter(), http.MethodPost, "/generate", c.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body=%s)", w.Code, w.Body.String())
			}
		})
	}
}

// CORS preflight: OPTIONS 는 미들웨어가 204 로 끊고 허용 헤더를 내려준다.
func TestCORS_Preflight(t *testing.T) {
	w := do(t, newTestRouter(), http.MethodOptions, "/review", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}
}

// 알 수 없는 경로는 404.
func TestUnknownRoute_404(t *testing.T) {
	w := do(t, newTestRouter(), http.MethodGet, "/no-such-route", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
