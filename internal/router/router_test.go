package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/limbs713/BE/internal/image"
	"github.com/limbs713/BE/internal/rag"
	"github.com/limbs713/BE/internal/router"
)

func TestNew_RegistersHealthAndCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := router.New(&rag.Service{}, &image.Service{})

	// 등록된 라우트가 동작하는지 (health)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/health = %d, want 200", w.Code)
	}

	// CORS 미들웨어가 모든 응답에 헤더를 붙이는지
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS 헤더 누락")
	}
}

func TestNew_PreflightShortCircuits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := router.New(&rag.Service{}, &image.Service{})

	req := httptest.NewRequest(http.MethodOptions, "/generate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Errorf("Allow-Methods 누락")
	}
}
