-- 0003_sensitive_schema.sql
-- 민감 주제(sensitive_events) / 논란 전례(sensitive_issues) 스키마.
-- 운영 DB(PostgreSQL 15.6, pgvector 0.7.0)에서 pg_dump --schema-only 로 추출해 정리.
-- RAG 검토(/review·/generate·/trends·/knowledge)가 의존하는 핵심 테이블이다.
-- 시드 데이터는 0004_sensitive_seed.sql 에서 적재한다.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS public.sensitive_events (
    id                  text NOT NULL,
    title               text NOT NULL,
    category            text,
    trigger_expressions jsonb,
    event_date          text,
    description         text,
    linked_issues       jsonb,
    embedding           public.vector(1536),
    CONSTRAINT sensitive_events_pkey PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS public.sensitive_issues (
    issue_id        text NOT NULL,
    region          text,
    category        text,
    title           text NOT NULL,
    issue_date      date,
    description     text,
    event_id        text,
    urls            jsonb,
    new_description text,
    embedding       public.vector(1536),
    CONSTRAINT sensitive_issues_pkey PRIMARY KEY (issue_id),
    CONSTRAINT fk_event FOREIGN KEY (event_id) REFERENCES public.sensitive_events(id)
);

-- pgvector HNSW 인덱스(코사인). searchVector 의 `embedding <=> $1::vector` 정렬에 사용.
CREATE INDEX IF NOT EXISTS sensitive_events_embedding_hnsw
    ON public.sensitive_events USING hnsw (embedding public.vector_cosine_ops);
CREATE INDEX IF NOT EXISTS sensitive_issues_embedding_hnsw
    ON public.sensitive_issues USING hnsw (embedding public.vector_cosine_ops);
