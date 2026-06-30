package handler

import (
	"context"

	"github.com/limbs713/BE/internal/rag"
)

// Service 는 핸들러가 의존하는 RAG 서비스 동작의 집합이다.
// 운영에서는 *rag.Service 가 이를 만족하며, 테스트에서는 가짜 구현을 주입한다.
type Service interface {
	Review(ctx context.Context, input string) (*rag.ReviewResult, error)
	SaveHistory(ctx context.Context, r *rag.ReviewResult, source string, latencyMs int) error
	ListHistory(ctx context.Context, limit, offset int) ([]rag.HistoryItem, int, error)
	HistoryStats(ctx context.Context) ([]rag.HistoryStatCard, error)
	GetHistory(ctx context.Context, id string) (*rag.ReviewResult, error)
	Trends(ctx context.Context, limit int) ([]rag.Trend, error)
	Generate(ctx context.Context, req rag.GenerateRequest) ([]rag.GenerateCandidate, error)
	ListEvents(ctx context.Context, limit, offset int) ([]rag.EventListItem, int, error)
	GetEvent(ctx context.Context, id string) (*rag.EventDetail, error)
	SyncKnowledge(ctx context.Context) (*rag.SyncResult, error)
	KnowledgeStatus(ctx context.Context) (*rag.KnowledgeStatus, error)
}
