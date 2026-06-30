package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// KnowledgeSync 는 embedding 이 비어 있는 민감 주제를 임베딩해 채우고
// 동기화 시각을 갱신합니다. (POST /knowledge/sync)
func KnowledgeSync(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, err := svc.SyncKnowledge(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// KnowledgeStatus 는 지식베이스 상태(마지막 동기화 시각, 건수)를 반환합니다.
// (GET /knowledge/status)
func KnowledgeStatus(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, err := svc.KnowledgeStatus(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	}
}
