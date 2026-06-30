package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/limbs713/BE/internal/rag"
)

// Events 는 민감 사건 목록을 페이징 조회합니다(GET /events).
// 쿼리 파라미터 limit(기본 20), offset(기본 0) 을 받습니다.
func Events(svc Service) gin.HandlerFunc {
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

		items, total, err := svc.ListEvents(c.Request.Context(), limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if items == nil {
			items = []rag.EventListItem{}
		}
		c.JSON(http.StatusOK, gin.H{"events": items, "total": total})
	}
}

// EventDetail 은 id 로 사건 상세(연결 전례 포함)를 반환합니다(GET /events/:id).
// 사건이 없으면 404 를 반환합니다.
func EventDetail(svc Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		detail, err := svc.GetEvent(c.Request.Context(), id)
		if err != nil {
			// 행이 없을 때만 404. DB 장애 등은 500 으로 구분한다(/history/:id 와 동일).
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": "해당 민감 사건을 찾을 수 없습니다"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, detail)
	}
}
