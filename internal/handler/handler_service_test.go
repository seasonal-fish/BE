package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/limbs713/BE/internal/handler"
	"github.com/limbs713/BE/internal/rag"
)

// fakeService 는 handler.Service 를 만족하는 결정적 가짜 구현이다.
type fakeService struct {
	review      *rag.ReviewResult
	reviewErr   error
	saveErr     error
	list        []rag.HistoryItem
	listTotal   int
	listErr     error
	stats       []rag.HistoryStatCard
	statsErr    error
	get         *rag.ReviewResult
	getErr      error
	trends      []rag.Trend
	trendsErr   error
	candidates  []rag.GenerateCandidate
	generateErr error
	events      []rag.EventListItem
	eventsTotal int
	eventsErr   error
	event       *rag.EventDetail
	eventErr    error
	sync        *rag.SyncResult
	syncErr     error
	status      *rag.KnowledgeStatus
	statusErr   error
}

func (f *fakeService) Review(ctx context.Context, input string) (*rag.ReviewResult, error) {
	return f.review, f.reviewErr
}
func (f *fakeService) SaveHistory(ctx context.Context, r *rag.ReviewResult, source string, latencyMs int) error {
	return f.saveErr
}
func (f *fakeService) ListHistory(ctx context.Context, limit, offset int) ([]rag.HistoryItem, int, error) {
	return f.list, f.listTotal, f.listErr
}
func (f *fakeService) HistoryStats(ctx context.Context) ([]rag.HistoryStatCard, error) {
	return f.stats, f.statsErr
}
func (f *fakeService) GetHistory(ctx context.Context, id string) (*rag.ReviewResult, error) {
	return f.get, f.getErr
}
func (f *fakeService) Trends(ctx context.Context, limit int) ([]rag.Trend, error) {
	return f.trends, f.trendsErr
}
func (f *fakeService) Generate(ctx context.Context, req rag.GenerateRequest) ([]rag.GenerateCandidate, error) {
	return f.candidates, f.generateErr
}
func (f *fakeService) ListEvents(ctx context.Context, limit, offset int) ([]rag.EventListItem, int, error) {
	return f.events, f.eventsTotal, f.eventsErr
}
func (f *fakeService) GetEvent(ctx context.Context, id string) (*rag.EventDetail, error) {
	return f.event, f.eventErr
}
func (f *fakeService) SyncKnowledge(ctx context.Context) (*rag.SyncResult, error) {
	return f.sync, f.syncErr
}
func (f *fakeService) KnowledgeStatus(ctx context.Context) (*rag.KnowledgeStatus, error) {
	return f.status, f.statusErr
}

// serve 는 단일 핸들러를 등록한 엔진에 요청을 흘려 응답을 돌려준다.
func serve(h gin.HandlerFunc, method, route, path, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Handle(method, route, h)
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestReviewHandler_Success(t *testing.T) {
	f := &fakeService{review: &rag.ReviewResult{ID: "rev_1", Input: "광고", Verdict: rag.Verdict{Score: 80}}}
	w := serve(handler.Review(f), http.MethodPost, "/review", "/review", `{"text":"광고"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "rev_1") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestReviewHandler_SaveErrorStillOK(t *testing.T) {
	f := &fakeService{review: &rag.ReviewResult{ID: "rev_1"}, saveErr: context.DeadlineExceeded}
	w := serve(handler.Review(f), http.MethodPost, "/review", "/review", `{"text":"광고"}`)
	if w.Code != http.StatusOK { // 저장 실패는 베스트에포트 — 응답은 200
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestReviewHandler_ServiceError(t *testing.T) {
	f := &fakeService{reviewErr: context.DeadlineExceeded}
	w := serve(handler.Review(f), http.MethodPost, "/review", "/review", `{"text":"광고"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHistoryHandler_Success(t *testing.T) {
	f := &fakeService{list: []rag.HistoryItem{{ID: "rev_1"}}, listTotal: 1}
	w := serve(handler.History(f), http.MethodGet, "/history", "/history?limit=5&offset=0", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHistoryHandler_NilNormalized(t *testing.T) {
	f := &fakeService{list: nil, listTotal: 0}
	w := serve(handler.History(f), http.MethodGet, "/history", "/history", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("nil items 정규화 실패: %s", w.Body.String())
	}
}

func TestHistoryHandler_Error(t *testing.T) {
	f := &fakeService{listErr: context.DeadlineExceeded}
	w := serve(handler.History(f), http.MethodGet, "/history", "/history", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHistoryStatsHandler(t *testing.T) {
	ok := &fakeService{stats: []rag.HistoryStatCard{{Label: "총 검토"}}}
	if w := serve(handler.HistoryStats(ok), http.MethodGet, "/history/stats", "/history/stats", ""); w.Code != http.StatusOK {
		t.Fatalf("ok status = %d", w.Code)
	}
	bad := &fakeService{statsErr: context.DeadlineExceeded}
	if w := serve(handler.HistoryStats(bad), http.MethodGet, "/history/stats", "/history/stats", ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("err status = %d", w.Code)
	}
}

func TestHistoryDetailHandler_Success(t *testing.T) {
	f := &fakeService{get: &rag.ReviewResult{ID: "rev_9"}}
	w := serve(handler.HistoryDetail(f), http.MethodGet, "/history/:id", "/history/rev_9", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "rev_9") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHistoryDetailHandler_NotFound(t *testing.T) {
	f := &fakeService{getErr: pgx.ErrNoRows}
	w := serve(handler.HistoryDetail(f), http.MethodGet, "/history/:id", "/history/nope", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHistoryDetailHandler_Error(t *testing.T) {
	f := &fakeService{getErr: context.DeadlineExceeded}
	w := serve(handler.HistoryDetail(f), http.MethodGet, "/history/:id", "/history/x", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestTrendsHandler(t *testing.T) {
	ok := &fakeService{trends: []rag.Trend{{Tag: "#광복절"}}}
	if w := serve(handler.Trends(ok), http.MethodGet, "/trends", "/trends?limit=3", ""); w.Code != http.StatusOK {
		t.Fatalf("ok status = %d", w.Code)
	}
	// nil 정규화
	nilT := &fakeService{trends: nil}
	if w := serve(handler.Trends(nilT), http.MethodGet, "/trends", "/trends", ""); !strings.Contains(w.Body.String(), `"trends":[]`) {
		t.Fatalf("nil 정규화 실패: %s", w.Body.String())
	}
	bad := &fakeService{trendsErr: context.DeadlineExceeded}
	if w := serve(handler.Trends(bad), http.MethodGet, "/trends", "/trends", ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("err status = %d", w.Code)
	}
}

func TestEventsHandler(t *testing.T) {
	ok := &fakeService{events: []rag.EventListItem{{ID: "evt_1", Title: "사건"}}, eventsTotal: 1}
	if w := serve(handler.Events(ok), http.MethodGet, "/events", "/events?limit=5&offset=0", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("ok status=%d body=%s", w.Code, w.Body.String())
	}
	// nil 정규화
	nilE := &fakeService{events: nil, eventsTotal: 0}
	if w := serve(handler.Events(nilE), http.MethodGet, "/events", "/events", ""); !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Fatalf("nil 정규화 실패: %s", w.Body.String())
	}
	bad := &fakeService{eventsErr: context.DeadlineExceeded}
	if w := serve(handler.Events(bad), http.MethodGet, "/events", "/events", ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("err status = %d", w.Code)
	}
}

func TestEventDetailHandler(t *testing.T) {
	ok := &fakeService{event: &rag.EventDetail{ID: "evt_9", Issues: []rag.EventIssue{}}}
	if w := serve(handler.EventDetail(ok), http.MethodGet, "/events/:id", "/events/evt_9", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "evt_9") {
		t.Fatalf("ok status=%d body=%s", w.Code, w.Body.String())
	}
	notFound := &fakeService{eventErr: pgx.ErrNoRows}
	if w := serve(handler.EventDetail(notFound), http.MethodGet, "/events/:id", "/events/nope", ""); w.Code != http.StatusNotFound {
		t.Fatalf("404 status = %d", w.Code)
	}
	bad := &fakeService{eventErr: context.DeadlineExceeded}
	if w := serve(handler.EventDetail(bad), http.MethodGet, "/events/:id", "/events/x", ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("err status = %d", w.Code)
	}
}

func TestGenerateHandler(t *testing.T) {
	ok := &fakeService{candidates: []rag.GenerateCandidate{{Text: "문구"}}}
	if w := serve(handler.Generate(ok), http.MethodPost, "/generate", "/generate", `{"product":"에코백"}`); w.Code != http.StatusOK {
		t.Fatalf("ok status = %d", w.Code)
	}
	bad := &fakeService{generateErr: context.DeadlineExceeded}
	if w := serve(handler.Generate(bad), http.MethodPost, "/generate", "/generate", `{"product":"에코백"}`); w.Code != http.StatusInternalServerError {
		t.Fatalf("err status = %d", w.Code)
	}
}

func TestKnowledgeHandlers(t *testing.T) {
	okSync := &fakeService{sync: &rag.SyncResult{Synced: 3}}
	if w := serve(handler.KnowledgeSync(okSync), http.MethodPost, "/knowledge/sync", "/knowledge/sync", ""); w.Code != http.StatusOK {
		t.Fatalf("sync ok status = %d", w.Code)
	}
	badSync := &fakeService{syncErr: context.DeadlineExceeded}
	if w := serve(handler.KnowledgeSync(badSync), http.MethodPost, "/knowledge/sync", "/knowledge/sync", ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("sync err status = %d", w.Code)
	}
	okStatus := &fakeService{status: &rag.KnowledgeStatus{
		LastSyncedAt: "2026-06-25T00:00:00Z",
		Tables:       map[string]rag.TableStatus{"sensitive_events": {Total: 96, Embedded: 96}},
	}}
	if w := serve(handler.KnowledgeStatus(okStatus), http.MethodGet, "/knowledge/status", "/knowledge/status", ""); w.Code != http.StatusOK {
		t.Fatalf("status ok = %d", w.Code)
	}
	badStatus := &fakeService{statusErr: context.DeadlineExceeded}
	if w := serve(handler.KnowledgeStatus(badStatus), http.MethodGet, "/knowledge/status", "/knowledge/status", ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("status err = %d", w.Code)
	}
}
