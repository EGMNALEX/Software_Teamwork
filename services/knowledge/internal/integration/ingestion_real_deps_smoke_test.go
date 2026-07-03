package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

const knowledgeIngestionSmokeGate = "KNOWLEDGE_INGESTION_SMOKE"

const (
	knowledgeIngestionSmokeFilename = "knowledge-ingestion-smoke.md"
	knowledgeIngestionSmokeMarker   = "A-027 ingestion smoke relay marker"
	knowledgeIngestionSmokeQuestion = "Which relay marker proves the A-027 ingestion smoke was indexed?"
)

type knowledgeIngestionSmokeConfig struct {
	knowledgeServiceBaseURL string
	serviceToken            string
	userID                  string
	timeout                 time.Duration
}

type knowledgeIngestionDocument struct {
	ID            string
	Status        string
	ChunkCount    int64
	ParserBackend string
	ErrorMessage  string
}

type knowledgeIngestionQuery struct {
	Results []knowledgeIngestionQueryResult
	Trace   struct {
		HitCount int `json:"hitCount"`
	} `json:"trace"`
}

type knowledgeIngestionQueryResult struct {
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	DocumentID      string `json:"documentId"`
	ChunkID         string `json:"chunkId"`
	ContentPreview  string `json:"contentPreview"`
}

func TestKnowledgeIngestionRealDepsSmoke(t *testing.T) {
	if os.Getenv(knowledgeIngestionSmokeGate) != "1" {
		t.Skip("set KNOWLEDGE_INGESTION_SMOKE=1 to run the Knowledge ingestion real-deps smoke")
	}

	cfg := loadKnowledgeIngestionSmokeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	assertKnowledgeAdapterReady(t, ctx, cfg)

	requestID := "req_knowledge_ingestion_smoke_" + safeIdentifierSuffix(newSmokeRunID(t))
	knowledgeBaseID := createKnowledgeIngestionKnowledgeBase(t, ctx, cfg, requestID+"_kb")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		deleteKnowledgeIngestionKnowledgeBase(cleanupCtx, t, cfg, "req_knowledge_ingestion_cleanup_"+knowledgeBaseID, knowledgeBaseID)
	})

	doc := uploadKnowledgeIngestionDocument(t, ctx, cfg, requestID+"_upload", knowledgeBaseID)
	if doc.Status == "uploaded" {
		t.Fatalf("Knowledge ingestion stage: upload stayed in uploaded status; set KNOWLEDGE_AUTO_START_INGESTION=true before starting the Knowledge adapter and restart it before running this smoke")
	}
	readyDoc := waitForKnowledgeIngestionDocumentReady(t, ctx, cfg, requestID+"_ready", knowledgeBaseID, doc.ID)
	if readyDoc.ChunkCount <= 0 {
		t.Fatalf("Knowledge ingestion stage: ready document chunkCount = %d, want > 0", readyDoc.ChunkCount)
	}
	if strings.TrimSpace(readyDoc.ParserBackend) == "" {
		t.Fatal("Knowledge ingestion stage: ready document did not expose parserBackend")
	}

	chunks := listKnowledgeIngestionChunks(t, ctx, cfg, requestID+"_chunks", readyDoc.ID)
	if len(chunks) == 0 {
		t.Fatal("Knowledge chunk stage: list chunks returned no items")
	}

	query := createKnowledgeIngestionQuery(t, ctx, cfg, requestID+"_query", knowledgeBaseID)
	assertKnowledgeIngestionQueryHit(t, query, knowledgeBaseID, readyDoc.ID)
}

func loadKnowledgeIngestionSmokeConfig(t *testing.T) knowledgeIngestionSmokeConfig {
	t.Helper()
	serviceToken := strings.TrimSpace(firstNonEmptyEnv("KNOWLEDGE_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"))
	missing := map[string]string{
		"KNOWLEDGE_SERVICE_TOKEN or INTERNAL_SERVICE_TOKEN": serviceToken,
	}
	var missingKeys []string
	for key, value := range missing {
		if strings.TrimSpace(value) == "" {
			missingKeys = append(missingKeys, key)
		}
	}
	sort.Strings(missingKeys)
	if len(missingKeys) > 0 {
		t.Fatalf("KNOWLEDGE_INGESTION_SMOKE=1 requires %s", strings.Join(missingKeys, ", "))
	}

	timeout := 3 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("KNOWLEDGE_INGESTION_SMOKE_TIMEOUT")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			t.Fatalf("KNOWLEDGE_INGESTION_SMOKE_TIMEOUT must be a positive duration")
		}
		timeout = value
	}

	baseURL := firstNonEmptyEnv("KNOWLEDGE_SERVICE_BASE_URL", "KNOWLEDGE_ADAPTER_BASE_URL")
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://127.0.0.1:8083"
	}
	userID := firstNonEmptyEnv("KNOWLEDGE_INGESTION_SMOKE_USER_ID", "KNOWLEDGE_INTEGRATION_USER_ID")
	if strings.TrimSpace(userID) == "" {
		userID = "knowledge_ingestion_smoke_user"
	}

	return knowledgeIngestionSmokeConfig{
		knowledgeServiceBaseURL: trimHTTPBaseURL(t, "KNOWLEDGE_SERVICE_BASE_URL", baseURL),
		serviceToken:            serviceToken,
		userID:                  strings.TrimSpace(userID),
		timeout:                 timeout,
	}
}

func assertKnowledgeAdapterReady(t *testing.T, ctx context.Context, cfg knowledgeIngestionSmokeConfig) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.knowledgeServiceBaseURL+"/readyz", nil)
	if err != nil {
		t.Fatalf("build Knowledge readyz request: %v", err)
	}
	req.Header.Set("X-Request-Id", "req_knowledge_ingestion_precheck")
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge readyz request failed: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("Knowledge readyz returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func createKnowledgeIngestionKnowledgeBase(t *testing.T, ctx context.Context, cfg knowledgeIngestionSmokeConfig, requestID string) string {
	t.Helper()
	suffix := safeIdentifierSuffix(newSmokeRunID(t))
	payload, err := json.Marshal(map[string]any{
		"name":        "Knowledge ingestion smoke " + suffix,
		"description": "A-027 upload to ready smoke",
	})
	if err != nil {
		t.Fatalf("encode Knowledge KB request: %v", err)
	}
	req, err := newKnowledgeIngestionRequest(ctx, cfg, http.MethodPost, "/internal/v1/knowledge-bases", bytes.NewReader(payload), requestID)
	if err != nil {
		t.Fatalf("build Knowledge KB request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge KB create request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		t.Fatalf("Knowledge KB create returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&decoded); err != nil {
		t.Fatalf("decode Knowledge KB response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("Knowledge KB response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	if strings.TrimSpace(decoded.Data.ID) == "" {
		t.Fatal("Knowledge KB response id is empty")
	}
	return strings.TrimSpace(decoded.Data.ID)
}

func deleteKnowledgeIngestionKnowledgeBase(ctx context.Context, t *testing.T, cfg knowledgeIngestionSmokeConfig, requestID string, knowledgeBaseID string) {
	t.Helper()
	req, err := newKnowledgeIngestionRequest(ctx, cfg, http.MethodDelete, "/internal/v1/knowledge-bases/"+url.PathEscape(knowledgeBaseID), nil, requestID)
	if err != nil {
		t.Errorf("cleanup Knowledge KB %q: build request: %v", knowledgeBaseID, err)
		return
	}
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Errorf("cleanup Knowledge KB %q: request failed: %v", knowledgeBaseID, err)
		return
	}
	defer res.Body.Close()
	discardResponse(res.Body)
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusNotFound {
		t.Errorf("cleanup Knowledge KB %q: DELETE returned HTTP %d", knowledgeBaseID, res.StatusCode)
	}
}

func uploadKnowledgeIngestionDocument(t *testing.T, ctx context.Context, cfg knowledgeIngestionSmokeConfig, requestID string, knowledgeBaseID string) knowledgeIngestionDocument {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", knowledgeIngestionSmokeFilename)
	if err != nil {
		t.Fatalf("create Knowledge upload file part: %v", err)
	}
	if _, err := part.Write([]byte(knowledgeIngestionFixtureText())); err != nil {
		t.Fatalf("write Knowledge upload fixture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close Knowledge upload multipart writer: %v", err)
	}

	path := "/internal/v1/knowledge-bases/" + url.PathEscape(knowledgeBaseID) + "/documents"
	req, err := newKnowledgeIngestionRequest(ctx, cfg, http.MethodPost, path, body, requestID)
	if err != nil {
		t.Fatalf("build Knowledge document upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge document upload request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		t.Fatalf("Knowledge document upload returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return decodeKnowledgeIngestionDocument(t, res.Body, requestID)
}

func knowledgeIngestionFixtureText() string {
	return "# Knowledge ingestion smoke\n\n" +
		knowledgeIngestionSmokeMarker + " must be searchable after deepdoc parses, chunks, embeds, and indexes this Markdown fixture.\n"
}

func waitForKnowledgeIngestionDocumentReady(t *testing.T, ctx context.Context, cfg knowledgeIngestionSmokeConfig, requestID string, knowledgeBaseID string, documentID string) knowledgeIngestionDocument {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(3 * time.Minute)
	}
	var last knowledgeIngestionDocument
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		doc := getKnowledgeIngestionDocument(t, ctx, cfg, fmt.Sprintf("%s_%02d", requestID, attempt), knowledgeBaseID, documentID)
		last = doc
		switch doc.Status {
		case "ready":
			return doc
		case "failed":
			t.Fatalf("Knowledge ingestion stage: document %s failed; error=%q", documentID, doc.ErrorMessage)
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Knowledge ingestion stage: document %s did not become ready before timeout; last status=%q chunkCount=%d. Check /internal/v1/runtime/status and knowledge-runtime worker logs.", documentID, last.Status, last.ChunkCount)
	return knowledgeIngestionDocument{}
}

func getKnowledgeIngestionDocument(t *testing.T, ctx context.Context, cfg knowledgeIngestionSmokeConfig, requestID string, knowledgeBaseID string, documentID string) knowledgeIngestionDocument {
	t.Helper()
	path := "/internal/v1/documents/" + url.PathEscape(documentID) + "?knowledgeBaseId=" + url.QueryEscape(knowledgeBaseID)
	req, err := newKnowledgeIngestionRequest(ctx, cfg, http.MethodGet, path, nil, requestID)
	if err != nil {
		t.Fatalf("build Knowledge document get request: %v", err)
	}
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge document get request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		t.Fatalf("Knowledge document get returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return decodeKnowledgeIngestionDocument(t, res.Body, requestID)
}

func decodeKnowledgeIngestionDocument(t *testing.T, body io.Reader, requestID string) knowledgeIngestionDocument {
	t.Helper()
	var decoded struct {
		Data struct {
			ID            string `json:"id"`
			Status        string `json:"status"`
			ChunkCount    int64  `json:"chunkCount"`
			ParserBackend string `json:"parserBackend"`
			ErrorMessage  string `json:"errorMessage"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 2<<20)).Decode(&decoded); err != nil {
		t.Fatalf("decode Knowledge document response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("Knowledge document response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	if strings.TrimSpace(decoded.Data.ID) == "" {
		t.Fatal("Knowledge document response id is empty")
	}
	return knowledgeIngestionDocument{
		ID:            strings.TrimSpace(decoded.Data.ID),
		Status:        strings.TrimSpace(decoded.Data.Status),
		ChunkCount:    decoded.Data.ChunkCount,
		ParserBackend: strings.TrimSpace(decoded.Data.ParserBackend),
		ErrorMessage:  strings.TrimSpace(decoded.Data.ErrorMessage),
	}
}

func listKnowledgeIngestionChunks(t *testing.T, ctx context.Context, cfg knowledgeIngestionSmokeConfig, requestID string, documentID string) []map[string]any {
	t.Helper()
	path := "/internal/v1/documents/" + url.PathEscape(documentID) + "/chunks?page=1&pageSize=5"
	req, err := newKnowledgeIngestionRequest(ctx, cfg, http.MethodGet, path, nil, requestID)
	if err != nil {
		t.Fatalf("build Knowledge chunks request: %v", err)
	}
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge chunks request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		t.Fatalf("Knowledge chunks returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		Data      []map[string]any `json:"data"`
		RequestID string           `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&decoded); err != nil {
		t.Fatalf("decode Knowledge chunks response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("Knowledge chunks response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	return decoded.Data
}

func createKnowledgeIngestionQuery(t *testing.T, ctx context.Context, cfg knowledgeIngestionSmokeConfig, requestID string, knowledgeBaseID string) knowledgeIngestionQuery {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"query":            knowledgeIngestionSmokeQuestion,
		"knowledgeBaseIds": []string{knowledgeBaseID},
		"topK":             3,
		"scoreThreshold":   0,
	})
	if err != nil {
		t.Fatalf("encode Knowledge query request: %v", err)
	}
	req, err := newKnowledgeIngestionRequest(ctx, cfg, http.MethodPost, "/internal/v1/knowledge-queries", bytes.NewReader(payload), requestID)
	if err != nil {
		t.Fatalf("build Knowledge query request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge query request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		t.Fatalf("Knowledge query returned HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		Data      knowledgeIngestionQuery `json:"data"`
		RequestID string                  `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&decoded); err != nil {
		t.Fatalf("decode Knowledge query response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("Knowledge query response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	return decoded.Data
}

func assertKnowledgeIngestionQueryHit(t *testing.T, query knowledgeIngestionQuery, knowledgeBaseID string, documentID string) {
	t.Helper()
	if query.Trace.HitCount == 0 || len(query.Results) == 0 {
		t.Fatalf("Knowledge retrieval stage: hitCount=%d len(results)=%d, want at least one hit", query.Trace.HitCount, len(query.Results))
	}
	for _, result := range query.Results {
		if result.KnowledgeBaseID == knowledgeBaseID && result.DocumentID == documentID {
			if strings.TrimSpace(result.ChunkID) == "" {
				t.Fatal("Knowledge retrieval stage: matching result has empty chunkId")
			}
			if !strings.Contains(result.ContentPreview, knowledgeIngestionSmokeMarker) {
				t.Fatalf("Knowledge retrieval stage: matching result does not contain marker %q", knowledgeIngestionSmokeMarker)
			}
			return
		}
	}
	t.Fatalf("Knowledge retrieval stage: no result matched kb=%s doc=%s", knowledgeBaseID, documentID)
}

func newKnowledgeIngestionRequest(ctx context.Context, cfg knowledgeIngestionSmokeConfig, method string, path string, body io.Reader, requestID string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, cfg.knowledgeServiceBaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Request-Id", requestID)
	req.Header.Set("X-Service-Token", cfg.serviceToken)
	req.Header.Set("X-User-Id", cfg.userID)
	req.Header.Set("X-User-Permissions", "knowledge:read,knowledge:write")
	return req, nil
}
