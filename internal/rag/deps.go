package rag

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgxPool 은 Service/store 가 사용하는 pgx 풀 메서드의 최소 집합이다.
// 운영에서는 *pgxpool.Pool 이, 테스트에서는 pgxmock 이 이 인터페이스를 만족한다.
type pgxPool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Ping(ctx context.Context) error
	Close()
}

// aiClient 는 Service 가 사용하는 OpenAI 호출의 집합이다.
// 운영에서는 *openAIClient 가, 테스트에서는 가짜 구현이 이 인터페이스를 만족한다.
type aiClient interface {
	Rewrite(ctx context.Context, query string) string
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Judge(ctx context.Context, system, user string) (string, error)
	Generate(ctx context.Context, product, tone string, trends []string) ([]string, error)
}
