package rag

import (
	"context"
	"fmt"
	"time"
)

// embedBatchSize 는 한 번에 임베딩 요청을 묶는 행 수입니다.
const embedBatchSize = 64

// embedSpec 는 임베딩 백필 대상 테이블 한 곳의 명세입니다.
//   - pk:      기본키 컬럼명 (조회·갱신 키)
//   - textSQL: 임베딩할 텍스트를 만드는 SQL 식 (NULL 방어는 COALESCE 로)
type embedSpec struct {
	table   string
	pk      string
	textSQL string
}

// embedSpecs 는 /knowledge/sync 와 재임베딩이 다루는 테이블 목록입니다.
// (ad_images 는 OCR 이미지라 임베딩 대상이 아니므로 제외.)
// textSQL 주의: 적재 임베딩(/knowledge/sync)과 수동 재임베딩(reembed_all.py)의
// 텍스트 구성은 글자 단위로 동일해야 한다(어긋나면 검색이 깨짐).
var embedSpecs = []embedSpec{
	// trigger_expressions(사용자 표면 표현)를 추가해 recall 보강. jsonb 비배열/NULL 방어.
	{"sensitive_events", "id", "COALESCE(title,'')||' '||COALESCE(description,'')||' '||CASE WHEN jsonb_typeof(trigger_expressions)='array' THEN array_to_string(ARRAY(SELECT jsonb_array_elements_text(trigger_expressions)),' ') ELSE '' END"},
	// description≈new_description 중복 제거: 정제된 정본(new_description) 우선, 비면 description.
	{"sensitive_issues", "issue_id", "COALESCE(title,'')||' '||COALESCE(NULLIF(new_description,''), description, '')"},
	// nuance(긍정/부정/중립 범주형)는 노이즈라 제외.
	{"slang_terms", "id", "COALESCE(expression,'')||' '||COALESCE(meaning,'')||' '||COALESCE(reason,'')"},
	{"mim_terms", "id", "COALESCE(word,'')||' '||COALESCE(definition,'')"},
}

// SyncResult 는 지식베이스 동기화 결과 요약입니다.
type SyncResult struct {
	Synced       int            `json:"synced"`   // 전 테이블 신규 임베딩 합계
	ByTable      map[string]int `json:"by_table"` // 테이블별 신규 임베딩 건수
	LastSyncedAt string         `json:"last_synced_at"`
}

// TableStatus 는 한 테이블의 임베딩 적재 현황입니다.
type TableStatus struct {
	Total    int `json:"total"`
	Embedded int `json:"embedded"`
}

// KnowledgeStatus 는 현재 지식베이스 상태(마지막 동기화 시각, 테이블별 적재 현황)입니다.
type KnowledgeStatus struct {
	LastSyncedAt string                 `json:"last_synced_at"`
	Tables       map[string]TableStatus `json:"tables"`
}

// pendingRow 는 embedding 이 비어 있어 백필 대상이 되는 한 행입니다.
type pendingRow struct {
	pk   string // pk 는 텍스트로 캐스팅해 타입(text/bigint) 차이를 흡수한다.
	text string
}

// SyncKnowledge 는 embedSpecs 의 모든 테이블에서 embedding 이 NULL 인 행만 임베딩해 채우고,
// kb_sync_meta.last_synced_at 을 갱신합니다. 대상이 0개여도 정상 동작합니다(idempotent).
func (s *Service) SyncKnowledge(ctx context.Context) (*SyncResult, error) {
	byTable := make(map[string]int, len(embedSpecs))
	total := 0
	for _, spec := range embedSpecs {
		n, err := s.backfillTable(ctx, spec)
		if err != nil {
			return nil, fmt.Errorf("%s 임베딩 실패: %w", spec.table, err)
		}
		byTable[spec.table] = n
		total += n
	}

	syncedAt, err := s.touchSyncMeta(ctx)
	if err != nil {
		return nil, fmt.Errorf("동기화 시각 갱신 실패: %w", err)
	}

	return &SyncResult{
		Synced:       total,
		ByTable:      byTable,
		LastSyncedAt: syncedAt.Format(time.RFC3339),
	}, nil
}

// backfillTable 은 한 테이블에서 embedding 이 NULL 인 행을 배치 임베딩해 채우고 처리 건수를 반환합니다.
func (s *Service) backfillTable(ctx context.Context, spec embedSpec) (int, error) {
	pending, err := s.pendingRows(ctx, spec)
	if err != nil {
		return 0, fmt.Errorf("임베딩 대상 조회 실패: %w", err)
	}

	synced := 0
	for start := 0; start < len(pending); start += embedBatchSize {
		end := min(start+embedBatchSize, len(pending))
		batch := pending[start:end]

		texts := make([]string, len(batch))
		for i, r := range batch {
			texts[i] = r.text
		}

		vecs, err := s.ai.EmbedBatch(ctx, texts)
		if err != nil {
			return synced, fmt.Errorf("임베딩 생성 실패: %w", err)
		}

		for i, r := range batch {
			if err := s.updateEmbedding(ctx, spec, r.pk, vecs[i]); err != nil {
				return synced, fmt.Errorf("임베딩 저장 실패(%s=%s): %w", spec.pk, r.pk, err)
			}
			synced++
		}
	}
	return synced, nil
}

// pendingRows 는 embedding 이 NULL 인 행을 (pk, 임베딩텍스트)로 조회합니다.
// 테이블/컬럼명은 embedSpecs 의 상수에서만 오므로 SQL 조합은 안전합니다.
func (s *Service) pendingRows(ctx context.Context, spec embedSpec) ([]pendingRow, error) {
	q := fmt.Sprintf(
		`SELECT %s::text, %s AS text FROM %s WHERE embedding IS NULL ORDER BY %s`,
		spec.pk, spec.textSQL, spec.table, spec.pk,
	)
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []pendingRow
	for rows.Next() {
		var r pendingRow
		if err := rows.Scan(&r.pk, &r.text); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// updateEmbedding 은 한 행의 embedding 컬럼을 갱신합니다.
func (s *Service) updateEmbedding(ctx context.Context, spec embedSpec, pk string, vec []float32) error {
	q := fmt.Sprintf(`UPDATE %s SET embedding = $1::vector WHERE %s::text = $2`, spec.table, spec.pk)
	_, err := s.pool.Exec(ctx, q, vectorLiteral(vec), pk)
	return err
}

// KnowledgeStatus 는 마지막 동기화 시각과 임베딩 테이블별 적재 현황을 반환합니다.
func (s *Service) KnowledgeStatus(ctx context.Context) (*KnowledgeStatus, error) {
	var lastSynced string
	var ts *time.Time
	const q = `SELECT last_synced_at FROM kb_sync_meta WHERE id = 1`
	if err := s.pool.QueryRow(ctx, q).Scan(&ts); err != nil {
		return nil, fmt.Errorf("동기화 메타 조회 실패: %w", err)
	}
	if ts != nil {
		lastSynced = ts.Format(time.RFC3339)
	}

	tables := make(map[string]TableStatus, len(embedSpecs))
	for _, spec := range embedSpecs {
		st, err := s.tableStatus(ctx, spec)
		if err != nil {
			return nil, fmt.Errorf("%s 현황 조회 실패: %w", spec.table, err)
		}
		tables[spec.table] = st
	}

	return &KnowledgeStatus{LastSyncedAt: lastSynced, Tables: tables}, nil
}

// tableStatus 는 한 테이블의 전체/임베딩완료 건수를 반환합니다.
func (s *Service) tableStatus(ctx context.Context, spec embedSpec) (TableStatus, error) {
	q := fmt.Sprintf(`SELECT COUNT(*), COUNT(embedding) FROM %s`, spec.table)
	var st TableStatus
	if err := s.pool.QueryRow(ctx, q).Scan(&st.Total, &st.Embedded); err != nil {
		return TableStatus{}, err
	}
	return st, nil
}

// touchSyncMeta 는 kb_sync_meta.last_synced_at 을 now() 로 갱신하고 그 시각을 반환합니다.
// 행이 없으면 새로 INSERT 합니다.
func (s *Service) touchSyncMeta(ctx context.Context) (time.Time, error) {
	const q = `
		INSERT INTO kb_sync_meta (id, last_synced_at)
		VALUES (1, now())
		ON CONFLICT (id) DO UPDATE SET last_synced_at = now()
		RETURNING last_synced_at`
	var ts time.Time
	if err := s.pool.QueryRow(ctx, q).Scan(&ts); err != nil {
		return time.Time{}, err
	}
	return ts, nil
}
