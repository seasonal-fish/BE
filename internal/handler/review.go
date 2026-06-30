package handler

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type reviewRequest struct {
	Text string `json:"text"`
}

// Review 는 광고 문구를 받아 RAG 검토(유사 주제 + 전례 + LLM 판정) 결과를 반환합니다.
// Service 의존성을 클로저로 주입받습니다.
func Review(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req reviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청 본문입니다"})
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "text 필드는 비어 있을 수 없습니다"})
			return
		}

		start := time.Now()
		result, err := svc.Review(c.Request.Context(), req.Text)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		latencyMs := int(time.Since(start).Milliseconds())

		// 검토 결과를 히스토리에 저장합니다(베스트에포트: 실패해도 응답은 막지 않음).
		// 응답 후 요청 컨텍스트가 취소될 수 있어 별도의 짧은 타임아웃 컨텍스트를 씁니다.
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := svc.SaveHistory(saveCtx, result, "text", latencyMs); err != nil {
			log.Printf("검토 히스토리 저장 실패: %v", err)
		}

		c.JSON(http.StatusOK, result)
	}
}
