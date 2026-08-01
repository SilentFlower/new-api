package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubCodeSearchUsesAnchorAndConfirmsFullTokenLocally(t *testing.T) {
	fullToken := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUV"
	anchor := fullToken[11:27]
	searchRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, githubCodeSearchAPIVersion, r.Header.Get("X-GitHub-Api-Version"))
		switch r.URL.Path {
		case "/search/code":
			searchRequests++
			assert.Equal(t, `"`+anchor+`"`, r.URL.Query().Get("q"))
			assert.NotContains(t, r.URL.RawQuery, fullToken)
			page, err := strconv.Atoi(r.URL.Query().Get("page"))
			require.NoError(t, err)
			if page == 1 {
				items := make([]map[string]any, githubCodeSearchPerPage)
				for index := range items {
					items[index] = map[string]any{
						"path": "private.txt",
						"sha":  "private-sha",
						"url":  serverURL(r) + "/contents/private",
						"repository": map[string]any{
							"id":        index + 1,
							"full_name": "private/repo",
							"private":   true,
						},
					}
				}
				writeJSONResponse(t, w, map[string]any{"total_count": 102, "incomplete_results": false, "items": items})
				return
			}
			writeJSONResponse(t, w, map[string]any{
				"total_count":        102,
				"incomplete_results": false,
				"items": []map[string]any{
					{
						"path":       "exact.txt",
						"sha":        "exact-sha",
						"url":        serverURL(r) + "/contents/exact",
						"html_url":   "https://github.com/public/repo/blob/ref/exact.txt",
						"repository": map[string]any{"id": 201, "full_name": "public/repo", "private": false},
					},
					{
						"path":       "similar.txt",
						"sha":        "similar-sha",
						"url":        serverURL(r) + "/contents/similar",
						"html_url":   "https://github.com/public/repo/blob/ref/similar.txt",
						"repository": map[string]any{"id": 201, "full_name": "public/repo", "private": false},
					},
				},
			})
		case "/contents/exact":
			writeContentResponse(t, w, "prefix "+fullToken+" suffix")
		case "/contents/similar":
			writeContentResponse(t, w, "prefix "+fullToken[:len(fullToken)-1]+"X suffix")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := newGitHubCodeSearchClient(server.URL, "test-token", server.Client(), time.Nanosecond)
	require.NoError(t, err)
	result, err := client.search(context.Background(), anchor, fullToken)
	require.NoError(t, err)

	assert.Equal(t, 2, searchRequests)
	assert.Equal(t, 2, result.SearchRequestCount)
	assert.Equal(t, githubCodeSearchPerPage, result.PrivateCandidateCount)
	assert.Equal(t, 2, result.CandidateCount)
	require.Len(t, result.Candidates, 1)
	assert.Equal(t, "exact.txt", result.Candidates[0].Path)
	assert.False(t, result.Incomplete)
}

func TestGitHubCodeSearchMarksUnknownVisibilityAndTruncationIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(t, w, map[string]any{
			"total_count":        githubCodeSearchMaxResults + 1,
			"incomplete_results": false,
			"items": []map[string]any{
				{
					"path":       "unknown.txt",
					"sha":        "unknown-sha",
					"url":        serverURL(r) + "/contents/unknown",
					"repository": map[string]any{"id": 301, "full_name": "unknown/repo"},
				},
			},
		})
	}))
	defer server.Close()

	client, err := newGitHubCodeSearchClient(server.URL, "test-token", server.Client(), time.Nanosecond)
	require.NoError(t, err)
	result, err := client.search(context.Background(), strings.Repeat("a", tokenLeakAnchorLength), strings.Repeat("a", 48))
	require.NoError(t, err)

	assert.True(t, result.Incomplete)
	assert.Equal(t, 1, result.VisibilityUnknownCount)
	assert.Equal(t, "visibility_unknown", result.IncompleteReasonCode)
}

func TestGitHubCodeSearchRetriesRecoverableFailures(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := newGitHubCodeSearchClient(server.URL, "test-token", server.Client(), time.Nanosecond)
	require.NoError(t, err)
	client.waitRetry = func(context.Context, time.Duration) error { return nil }
	result, err := client.search(context.Background(), strings.Repeat("a", tokenLeakAnchorLength), strings.Repeat("a", 48))
	require.Error(t, err)

	assert.Equal(t, githubCodeSearchMaxAttempts, attempts)
	assert.Equal(t, githubCodeSearchMaxAttempts, result.SearchRequestCount)
	assert.Equal(t, "github_unavailable", err.Error())
}

func TestGitHubCodeSearchRetriesCandidateDownloads(t *testing.T) {
	fullToken := strings.Repeat("c", 48)
	downloadAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/code":
			writeJSONResponse(t, w, map[string]any{
				"total_count":        1,
				"incomplete_results": false,
				"items": []map[string]any{{
					"path":       "leak.txt",
					"sha":        "leak-sha",
					"url":        serverURL(r) + "/contents/leak",
					"html_url":   "https://github.com/public/repo/blob/main/leak.txt",
					"repository": map[string]any{"id": 401, "full_name": "public/repo", "private": false},
				}},
			})
		case "/contents/leak":
			downloadAttempts++
			if downloadAttempts < githubCodeSearchMaxAttempts {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeContentResponse(t, w, fullToken)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := newGitHubCodeSearchClient(server.URL, "test-token", server.Client(), time.Nanosecond)
	require.NoError(t, err)
	client.waitRetry = func(context.Context, time.Duration) error { return nil }
	result, err := client.search(context.Background(), fullToken[:tokenLeakAnchorLength], fullToken)
	require.NoError(t, err)
	assert.Equal(t, githubCodeSearchMaxAttempts, downloadAttempts)
	require.Len(t, result.Candidates, 1)
	assert.False(t, result.Incomplete)
}

func TestGitHubCodeSearchStopsOnCandidateAuthenticationFailure(t *testing.T) {
	fullToken := strings.Repeat("d", 48)
	downloadAttempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/code" {
			writeJSONResponse(t, w, map[string]any{
				"total_count":        1,
				"incomplete_results": false,
				"items": []map[string]any{{
					"path":       "leak.txt",
					"sha":        "leak-sha",
					"url":        serverURL(r) + "/contents/leak",
					"repository": map[string]any{"id": 402, "full_name": "public/repo", "private": false},
				}},
			})
			return
		}
		downloadAttempts++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := newGitHubCodeSearchClient(server.URL, "test-token", server.Client(), time.Nanosecond)
	require.NoError(t, err)
	result, err := client.search(context.Background(), fullToken[:tokenLeakAnchorLength], fullToken)
	require.Error(t, err)
	assert.Equal(t, "auth_failed", err.Error())
	assert.Equal(t, 1, downloadAttempts)
	assert.Equal(t, 1, result.SearchRequestCount)
	assert.False(t, result.Incomplete)
}

func TestGitHubCodeSearchMarksDeletedCandidateIncomplete(t *testing.T) {
	fullToken := strings.Repeat("f", 48)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/code" {
			writeJSONResponse(t, w, map[string]any{
				"total_count":        1,
				"incomplete_results": false,
				"items": []map[string]any{{
					"path":       "deleted.txt",
					"sha":        "deleted-sha",
					"url":        serverURL(r) + "/contents/deleted",
					"repository": map[string]any{"id": 404, "full_name": "public/repo", "private": false},
				}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := newGitHubCodeSearchClient(server.URL, "test-token", server.Client(), time.Nanosecond)
	require.NoError(t, err)
	result, err := client.search(context.Background(), fullToken[:tokenLeakAnchorLength], fullToken)
	require.NoError(t, err)
	assert.True(t, result.Incomplete)
	assert.Equal(t, 1, result.DownloadFailureCount)
	assert.Equal(t, "candidate_not_found", result.IncompleteReasonCode)
	assert.Empty(t, result.Candidates)
}

func TestGitHubCodeSearchChecksScanStateBeforeCandidateDownload(t *testing.T) {
	fullToken := strings.Repeat("e", 48)
	downloadRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/code" {
			writeJSONResponse(t, w, map[string]any{
				"total_count":        1,
				"incomplete_results": false,
				"items": []map[string]any{{
					"path":       "leak.txt",
					"sha":        "leak-sha",
					"url":        serverURL(r) + "/contents/leak",
					"repository": map[string]any{"id": 403, "full_name": "public/repo", "private": false},
				}},
			})
			return
		}
		downloadRequests++
		writeContentResponse(t, w, fullToken)
	}))
	defer server.Close()

	client, err := newGitHubCodeSearchClient(server.URL, "test-token", server.Client(), time.Nanosecond)
	require.NoError(t, err)
	checks := 0
	client.beforeRequest = func(context.Context) error {
		checks++
		if checks >= 3 {
			return ErrTokenLeakScanDisabled
		}
		return nil
	}
	_, err = client.search(context.Background(), fullToken[:tokenLeakAnchorLength], fullToken)
	require.ErrorIs(t, err, ErrTokenLeakScanDisabled)
	assert.Zero(t, downloadRequests)
}

func TestNormalizeGitHubHTMLURLRejectsExternalHost(t *testing.T) {
	assert.Equal(t, "https://github.com/public/repo", normalizeGitHubHTMLURL("https://example.com/leak", "public/repo"))
	assert.Equal(t, "https://github.com/public/repo/blob/main/key.txt", normalizeGitHubHTMLURL("https://github.com/public/repo/blob/main/key.txt", "public/repo"))
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func writeContentResponse(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	writeJSONResponse(t, w, map[string]any{
		"type":     "file",
		"encoding": "base64",
		"content":  base64.StdEncoding.EncodeToString([]byte(content)),
		"size":     len(content),
	})
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	_, err = fmt.Fprint(w, string(data))
	require.NoError(t, err)
}
