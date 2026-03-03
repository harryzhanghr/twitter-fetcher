package twitter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.x.com/2"

// RateLimitError is returned when the API responds with HTTP 429.
// ResetAt is parsed from the x-rate-limit-reset header.
type RateLimitError struct {
	ResetAt time.Time
}

func (e RateLimitError) Error() string {
	if e.ResetAt.IsZero() {
		return "twitter: rate limit exceeded (429)"
	}
	return fmt.Sprintf("twitter: rate limit exceeded (429), resets at %s", e.ResetAt.Format(time.RFC3339))
}

// Client wraps the Twitter API v2 with OAuth2 token management.
type Client struct {
	http          *http.Client
	tokenProvider TokenProvider
}

// NewClient creates a new API client using the given token provider.
func NewClient(tokenProvider TokenProvider) *Client {
	return &Client{
		http:          &http.Client{},
		tokenProvider: tokenProvider,
	}
}

// GetUserTweets fetches tweets for the given user, applying cursor and filter params.
// It excludes replies but keeps retweets (intentionally different from llm-data reference).
// Token freshness is managed proactively by OAuth2TokenProvider (5-min expiry buffer).
// A 401 from the tweets endpoint means the account is inaccessible (protected/suspended),
// not that the token is bad — so we never retry on 401 to avoid refresh-token cascade.
func (c *Client) GetUserTweets(ctx context.Context, req UserTweetsRequest) (*UserTweetsResponse, error) {
	token, err := c.tokenProvider.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/users/%s/tweets", baseURL, req.UserID)

	params := url.Values{}
	params.Set("tweet.fields", "id,text,author_id,created_at,referenced_tweets,public_metrics,conversation_id,entities,attachments")
	params.Set("expansions", "referenced_tweets.id,referenced_tweets.id.author_id,author_id,attachments.media_keys")
	params.Set("user.fields", "id,name,username")
	params.Set("media.fields", "media_key,type,url,preview_image_url,variants")
	params.Set("exclude", "replies") // keep retweets; only exclude replies
	if req.MaxResults > 0 {
		params.Set("max_results", strconv.Itoa(req.MaxResults))
	}
	if req.SinceID != "" {
		params.Set("since_id", req.SinceID)
	} else if req.StartTime != "" {
		params.Set("start_time", req.StartTime)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		var resetAt time.Time
		if v := resp.Header.Get("x-rate-limit-reset"); v != "" {
			if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
				resetAt = time.Unix(ts, 0)
			}
		}
		return nil, RateLimitError{ResetAt: resetAt}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for user %s", resp.StatusCode, req.UserID)
	}

	var result UserTweetsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetTweets fetches tweets by ID in bulk (up to 100 per call).
// Only requests public_metrics to minimise payload.
func (c *Client) GetTweets(ctx context.Context, ids []string) (*TweetLookupResponse, error) {
	if len(ids) == 0 {
		return &TweetLookupResponse{}, nil
	}
	if len(ids) > 100 {
		return nil, fmt.Errorf("GetTweets: max 100 ids per request, got %d", len(ids))
	}

	token, err := c.tokenProvider.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	params := url.Values{}
	params.Set("ids", strings.Join(ids, ","))
	params.Set("tweet.fields", "public_metrics")

	endpoint := baseURL + "/tweets"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		var resetAt time.Time
		if v := resp.Header.Get("x-rate-limit-reset"); v != "" {
			if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
				resetAt = time.Unix(ts, 0)
			}
		}
		return nil, RateLimitError{ResetAt: resetAt}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for tweet lookup", resp.StatusCode)
	}

	var result TweetLookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
