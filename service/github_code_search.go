package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	githubCodeSearchBaseURL          = "https://api.github.com"
	githubCodeSearchAPIVersion       = "2026-03-10"
	githubCodeSearchPerPage          = 100
	githubCodeSearchMaxResults       = 1000
	githubCodeSearchResponseMaxBytes = 8 << 20
	githubContentResponseMaxBytes    = 2 << 20
	githubCodeSearchMaxAttempts      = 3
)

type githubCodeSearchError struct {
	code       string
	statusCode int
	fatal      bool
	retryable  bool
}

func (err *githubCodeSearchError) Error() string {
	return err.code
}

type githubCodeSearchLimiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func (limiter *githubCodeSearchLimiter) wait(ctx context.Context) error {
	limiter.mu.Lock()
	now := time.Now()
	target := now
	if limiter.next.After(now) {
		target = limiter.next
	}
	limiter.next = target.Add(limiter.interval)
	limiter.mu.Unlock()

	waitDuration := time.Until(target)
	if waitDuration <= 0 {
		return nil
	}
	timer := time.NewTimer(waitDuration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type githubCodeSearchRepository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  *bool  `json:"private"`
}

type githubCodeSearchItem struct {
	Path       string                     `json:"path"`
	SHA        string                     `json:"sha"`
	URL        string                     `json:"url"`
	HTMLURL    string                     `json:"html_url"`
	Repository githubCodeSearchRepository `json:"repository"`
}

type githubCodeSearchResponse struct {
	TotalCount        int                    `json:"total_count"`
	IncompleteResults bool                   `json:"incomplete_results"`
	Items             []githubCodeSearchItem `json:"items"`
}

type githubContentResponse struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
}

type githubCodeCandidate struct {
	RepositoryID   int64
	RepositoryName string
	Path           string
	SHA            string
	HTMLURL        string
}

type githubCodeSearchResult struct {
	Candidates             []githubCodeCandidate
	SearchRequestCount     int
	CandidateCount         int
	PrivateCandidateCount  int
	DownloadFailureCount   int
	VisibilityUnknownCount int
	Incomplete             bool
	IncompleteReasonCode   string
}

type githubCodeSearchClient struct {
	baseURL       *url.URL
	httpClient    *http.Client
	token         string
	limiter       *githubCodeSearchLimiter
	beforeRequest func(context.Context) error
	waitRetry     func(context.Context, time.Duration) error
}

func newGitHubCodeSearchClient(baseURL string, token string, httpClient *http.Client, interval time.Duration) (*githubCodeSearchClient, error) {
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, errors.New("github_base_url_invalid")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("github_token_missing")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if interval <= 0 {
		interval = time.Minute / 8
	}
	return &githubCodeSearchClient{
		baseURL:    parsedBaseURL,
		httpClient: httpClient,
		token:      token,
		limiter:    &githubCodeSearchLimiter{interval: interval},
		beforeRequest: func(ctx context.Context) error {
			return ctx.Err()
		},
		waitRetry: func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}, nil
}

func (client *githubCodeSearchClient) search(ctx context.Context, anchor string, fullToken string) (githubCodeSearchResult, error) {
	result := githubCodeSearchResult{Candidates: make([]githubCodeCandidate, 0)}
	for page := 1; page <= githubCodeSearchMaxResults/githubCodeSearchPerPage; page++ {
		response, requestCount, err := client.searchPage(ctx, anchor, page)
		result.SearchRequestCount += requestCount
		if err != nil {
			return result, err
		}
		if response.IncompleteResults {
			result.Incomplete = true
			result.IncompleteReasonCode = "search_incomplete"
		}
		if response.TotalCount > githubCodeSearchMaxResults {
			result.Incomplete = true
			result.IncompleteReasonCode = "search_truncated"
		}

		for _, item := range response.Items {
			if item.Repository.Private == nil {
				result.VisibilityUnknownCount++
				result.Incomplete = true
				result.IncompleteReasonCode = "visibility_unknown"
				continue
			}
			if *item.Repository.Private {
				result.PrivateCandidateCount++
				continue
			}
			result.CandidateCount++
			content, fetchErr := client.fetchCandidate(ctx, item.URL)
			if fetchErr != nil {
				var githubErr *githubCodeSearchError
				if !errors.As(fetchErr, &githubErr) || githubErr.fatal {
					return result, fetchErr
				}
				result.DownloadFailureCount++
				result.Incomplete = true
				result.IncompleteReasonCode = githubErr.code
				continue
			}
			if !strings.Contains(content, fullToken) {
				continue
			}
			htmlURL := normalizeGitHubHTMLURL(item.HTMLURL, item.Repository.FullName)
			result.Candidates = append(result.Candidates, githubCodeCandidate{
				RepositoryID:   item.Repository.ID,
				RepositoryName: item.Repository.FullName,
				Path:           item.Path,
				SHA:            item.SHA,
				HTMLURL:        htmlURL,
			})
		}

		if len(response.Items) < githubCodeSearchPerPage || page*githubCodeSearchPerPage >= response.TotalCount {
			break
		}
	}
	return result, nil
}

func (client *githubCodeSearchClient) searchPage(ctx context.Context, anchor string, page int) (*githubCodeSearchResponse, int, error) {
	requestCount := 0
	for attempt := 1; attempt <= githubCodeSearchMaxAttempts; attempt++ {
		if err := client.beforeRequest(ctx); err != nil {
			return nil, requestCount, err
		}
		if err := client.limiter.wait(ctx); err != nil {
			return nil, requestCount, err
		}
		if err := client.beforeRequest(ctx); err != nil {
			return nil, requestCount, err
		}
		requestCount++
		response, retryAfter, err := client.doSearchPage(ctx, anchor, page)
		if err == nil {
			return response, requestCount, nil
		}
		var searchErr *githubCodeSearchError
		if !errors.As(err, &searchErr) || searchErr.fatal || !searchErr.retryable || attempt == githubCodeSearchMaxAttempts {
			return nil, requestCount, err
		}
		if retryAfter <= 0 {
			retryAfter = time.Duration(attempt) * time.Second
		}
		if waitErr := client.waitRetry(ctx, retryAfter); waitErr != nil {
			return nil, requestCount, waitErr
		}
	}
	return nil, requestCount, &githubCodeSearchError{code: "search_failed"}
}

func (client *githubCodeSearchClient) doSearchPage(ctx context.Context, anchor string, page int) (*githubCodeSearchResponse, time.Duration, error) {
	requestURL := *client.baseURL
	requestURL.Path = strings.TrimRight(client.baseURL.Path, "/") + "/search/code"
	query := requestURL.Query()
	query.Set("q", `"`+anchor+`"`)
	query.Set("per_page", strconv.Itoa(githubCodeSearchPerPage))
	query.Set("page", strconv.Itoa(page))
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, 0, &githubCodeSearchError{code: "request_invalid", fatal: true}
	}
	client.setGitHubHeaders(request)
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, &githubCodeSearchError{code: "network_error", retryable: true}
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, parseRetryAfter(response.Header.Get("Retry-After")), classifyGitHubStatus(response.StatusCode)
	}
	body, readErr := readLimitedBody(response.Body, githubCodeSearchResponseMaxBytes)
	if readErr != nil {
		return nil, 0, &githubCodeSearchError{code: "response_too_large", fatal: true}
	}
	var payload githubCodeSearchResponse
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil, 0, &githubCodeSearchError{code: "invalid_response", fatal: true}
	}
	return &payload, 0, nil
}

func (client *githubCodeSearchClient) fetchCandidate(ctx context.Context, candidateURL string) (string, error) {
	for attempt := 1; attempt <= githubCodeSearchMaxAttempts; attempt++ {
		if err := client.beforeRequest(ctx); err != nil {
			return "", err
		}
		content, retryAfter, err := client.doFetchCandidate(ctx, candidateURL)
		if err == nil {
			return content, nil
		}
		var fetchErr *githubCodeSearchError
		if !errors.As(err, &fetchErr) || fetchErr.fatal || !fetchErr.retryable || attempt == githubCodeSearchMaxAttempts {
			return "", err
		}
		if retryAfter <= 0 {
			retryAfter = time.Duration(attempt) * time.Second
		}
		if waitErr := client.waitRetry(ctx, retryAfter); waitErr != nil {
			return "", waitErr
		}
	}
	return "", &githubCodeSearchError{code: "candidate_fetch_failed"}
}

func (client *githubCodeSearchClient) doFetchCandidate(ctx context.Context, candidateURL string) (string, time.Duration, error) {
	parsedURL, err := url.Parse(candidateURL)
	if err != nil || parsedURL.Scheme != client.baseURL.Scheme || parsedURL.Host != client.baseURL.Host {
		return "", 0, &githubCodeSearchError{code: "candidate_url_invalid", fatal: true}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return "", 0, &githubCodeSearchError{code: "candidate_request_invalid", fatal: true}
	}
	client.setGitHubHeaders(request)
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		return "", 0, &githubCodeSearchError{code: "candidate_fetch_failed", retryable: true}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusNotFound {
			return "", 0, &githubCodeSearchError{code: "candidate_not_found", statusCode: response.StatusCode}
		}
		return "", parseRetryAfter(response.Header.Get("Retry-After")), classifyGitHubStatus(response.StatusCode)
	}
	body, readErr := readLimitedBody(response.Body, githubContentResponseMaxBytes)
	if readErr != nil {
		return "", 0, &githubCodeSearchError{code: "candidate_response_too_large"}
	}
	var payload githubContentResponse
	if err := common.Unmarshal(body, &payload); err != nil {
		return "", 0, &githubCodeSearchError{code: "candidate_response_invalid"}
	}
	if payload.Type != "file" || payload.Encoding != "base64" || payload.Size > 384*1024 {
		return "", 0, &githubCodeSearchError{code: "candidate_content_invalid"}
	}
	encodedContent := strings.ReplaceAll(payload.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(encodedContent)
	if err != nil || int64(len(decoded)) > 384*1024 {
		return "", 0, &githubCodeSearchError{code: "candidate_content_invalid"}
	}
	return string(decoded), 0, nil
}

func (client *githubCodeSearchClient) setGitHubHeaders(request *http.Request) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("X-GitHub-Api-Version", githubCodeSearchAPIVersion)
	request.Header.Set("User-Agent", "new-api-token-leak-scan")
}

func classifyGitHubStatus(statusCode int) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return &githubCodeSearchError{code: "auth_failed", statusCode: statusCode, fatal: true}
	case http.StatusForbidden, http.StatusTooManyRequests:
		return &githubCodeSearchError{code: "rate_limited", statusCode: statusCode, retryable: true}
	case http.StatusUnprocessableEntity:
		return &githubCodeSearchError{code: "search_rejected", statusCode: statusCode, fatal: true}
	default:
		if statusCode >= 500 {
			return &githubCodeSearchError{code: "github_unavailable", statusCode: statusCode, retryable: true}
		}
		return &githubCodeSearchError{code: fmt.Sprintf("github_http_%d", statusCode), statusCode: statusCode, fatal: true}
	}
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		if duration := time.Until(retryAt); duration > 0 {
			return duration
		}
	}
	return 0
}

func readLimitedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, errors.New("response_too_large")
	}
	return body, nil
}

func normalizeGitHubHTMLURL(rawURL string, repositoryName string) string {
	parsedURL, err := url.Parse(rawURL)
	if err == nil && parsedURL.Scheme == "https" && parsedURL.Hostname() == "github.com" && parsedURL.User == nil {
		return parsedURL.String()
	}
	parts := strings.Split(repositoryName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "https://github.com"
	}
	fallback := &url.URL{Scheme: "https", Host: "github.com", Path: "/" + parts[0] + "/" + parts[1]}
	return fallback.String()
}
