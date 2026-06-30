package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/limbs713/BE/internal/rag"
)

// History 는 검토 히스토리 목록을 페이징 조회합니다(GET /history).
// 쿼리 파라미터 limit(기본 20), offset(기본 0) 을 받습니다.
func History(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 20
		if v := c.Query("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		offset := 0
		if v := c.Query("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		items, total, err := svc.ListHistory(c.Request.Context(), limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if items == nil {
			items = []rag.HistoryItem{}
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
	}
}

// HistoryStats 는 히스토리 화면 상단 요약 카드를 반환합니다(GET /history/stats).
func HistoryStats(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		cards, err := svc.HistoryStats(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"cards": cards})
	}
}

// HistoryDetail 은 id 로 저장된 검토 결과 전체를 반환합니다(GET /history/:id).
// 없으면 404 를 반환합니다.
func HistoryDetail(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		result, err := svc.GetHistory(c.Request.Context(), id)
		if err != nil {
			// 행이 없을 때만 404. DB 장애·손상 JSON 등은 500 으로 구분해
			// 실제 서버 결함이 '없음'으로 가려지지 않게 한다.
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": "해당 검토 히스토리를 찾을 수 없습니다"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
