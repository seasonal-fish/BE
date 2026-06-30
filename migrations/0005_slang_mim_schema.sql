-- 0005_slang_mim_schema.sql
-- slang_terms(신조어) / mim_terms(유행어) 테이블 스키마 + 임베딩 컬럼.
-- 운영 DB에서 pg_dump --schema-only 로 추출해 idempotent 하게 정리.
-- /knowledge/sync(NULL 백필)와 재임베딩 스크립트가 이 테이블들의 embedding 을 채운다.
--
-- 신규 DB: CREATE TABLE 로 임베딩 컬럼까지 생성된다.
-- 운영 DB: 테이블이 이미 있으면 CREATE 는 건너뛰고, ALTER ADD COLUMN IF NOT EXISTS 로
--          mim_terms.embedding(운영엔 아직 없음)을 보강한다.

CREATE EXTENSION IF NOT EXISTS vector;

-- ── slang_terms ─────────────────────────────────────────────
CREATE SEQUENCE IF NOT EXISTS public.slang_terms_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.slang_terms (
    id         bigint NOT NULL DEFAULT nextval('public.slang_terms_id_seq'::regclass),
    expression text   NOT NULL,
    meaning    text   NOT NULL,
    nuance     text   NOT NULL,
    embedding  public.vector(1536),
    reason     text,
    CONSTRAINT slang_terms_pkey PRIMARY KEY (id),
    CONSTRAINT slang_terms_nuance_check CHECK (nuance = ANY (ARRAY['긍정'::text, '부정'::text, '중립'::text]))
);
ALTER TABLE IF EXISTS public.slang_terms ADD COLUMN IF NOT EXISTS embedding public.vector(1536);
CREATE INDEX IF NOT EXISTS slang_terms_embedding_hnsw
    ON public.slang_terms USING hnsw (embedding public.vector_cosine_ops);

-- ── mim_terms ───────────────────────────────────────────────
CREATE SEQUENCE IF NOT EXISTS public.mim_terms_id_seq
    START WITH 1 INCREMENT BY 1 NO MINVALUE NO MAXVALUE CACHE 1;

CREATE TABLE IF NOT EXISTS public.mim_terms (
    id                      bigint NOT NULL DEFAULT nextval('public.mim_terms_id_seq'::regclass),
    word                    character varying(50),
    definition              character varying(256),
    avg_search_ratio_7d     numeric(6,2),
    search_trend_updated_at timestamp with time zone,
    embedding               public.vector(1536),
    CONSTRAINT mim_terms_pkey PRIMARY KEY (id)
);
-- 운영 DB의 mim_terms 에는 embedding 컬럼이 없으므로 보강한다.
ALTER TABLE IF EXISTS public.mim_terms ADD COLUMN IF NOT EXISTS embedding public.vector(1536);
CREATE INDEX IF NOT EXISTS mim_terms_embedding_hnsw
    ON public.mim_terms USING hnsw (embedding public.vector_cosine_ops);
