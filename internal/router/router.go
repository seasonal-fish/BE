package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/limbs713/BE/internal/handler"
	"github.com/limbs713/BE/internal/image"
	"github.com/limbs713/BE/internal/rag"
)

// New creates and configures the Gin engine with all routes registered.
// 새로운 라우트는 여기에 추가하세요.
func New(ragSvc *rag.Service, imageSvc *image.Service) *gin.Engine {
	r := gin.Default()
	r.Use(cors())

	r.GET("/health", handler.Health)

	// 검토
	r.POST("/review", handler.Review(ragSvc))
	r.POST("/upload-image", handler.UploadImage(imageSvc))

	// 히스토리 (정적 경로 /history/stats 를 :id 보다 먼저 등록)
	r.GET("/history", handler.History(ragSvc))
	r.GET("/history/stats", handler.HistoryStats(ragSvc))
	r.GET("/history/:id", handler.HistoryDetail(ragSvc))

	// 민감 사건
	r.GET("/events", handler.Events(ragSvc))
	r.GET("/events/:id", handler.EventDetail(ragSvc))

	// 생성
	r.GET("/trends", handler.Trends(ragSvc))
	r.POST("/generate", handler.Generate(ragSvc))

	// 지식베이스 동기화
	r.POST("/knowledge/sync", handler.KnowledgeSync(ragSvc))
	r.GET("/knowledge/status", handler.KnowledgeStatus(ragSvc))

	return r
}

// cors 는 프론트엔드(별도 오리진)에서의 호출을 허용하는 최소 CORS 미들웨어입니다.
// 별도 의존성 없이 표준 헤더만 설정하고 preflight(OPTIONS)를 처리합니다.
func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
