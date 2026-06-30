package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/limbs713/BE/internal/rag"
)

// Trends 는 트렌드어 상위 N개를 반환합니다. (GET /trends)
// limit 쿼리 파라미터로 개수를 조절하며 기본값은 12입니다.
func Trends(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 12
		if raw := c.Query("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}

		trends, err := svc.Trends(c.Request.Context(), limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// nil → JSON null 방지(/history 와 동일하게 빈 배열로 정규화).
		if trends == nil {
			trends = []rag.Trend{}
		}
		c.JSON(http.StatusOK, gin.H{"trends": trends})
	}
}

// Generate 는 제품/톤/트렌드어로 광고 문구 후보를 생성하고 자동 검토 결과와 함께 반환합니다. (POST /generate)
func Generate(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req rag.GenerateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "잘못된 요청 본문입니다"})
			return
		}
		if strings.TrimSpace(req.Product) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "product 필드는 비어 있을 수 없습니다"})
			return
		}

		candidates, err := svc.Generate(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"candidates": candidates})
	}
}
