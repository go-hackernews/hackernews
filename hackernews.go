// Package hackernews is a dependency-free client for the official Hacker News
// Firebase API (https://hacker-news.firebaseio.com/v0/).
//
// It uses only the Go standard library and builds with CGO_ENABLED=0.
package hackernews

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// DefaultBaseURL is the base URL of the public Hacker News Firebase API.
const DefaultBaseURL = "https://hacker-news.firebaseio.com/v0"

// DefaultUserAgent is sent on every request unless overridden.
const DefaultUserAgent = "go-hackernews/hackernews (+https://github.com/go-hackernews/hackernews)"

// Client is a Hacker News API client. Use New to construct one.
type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	UserAgent  string
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient sets the underlying *http.Client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.HTTPClient = h }
}

// WithBaseURL overrides the API base URL (useful for testing).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.BaseURL = u }
}

// WithUserAgent overrides the User-Agent header sent on each request.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.UserAgent = ua }
}

// New returns a Client with sane defaults (the public API base URL and
// http.DefaultClient), then applies opts in order.
func New(opts ...Option) *Client {
	c := &Client{
		HTTPClient: http.DefaultClient,
		BaseURL:    DefaultBaseURL,
		UserAgent:  DefaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Item is a Hacker News item: a story, comment, job, poll, or poll option.
type Item struct {
	ID          int    `json:"id"`
	Type        string `json:"type"` // "story","comment","job","poll",...
	By          string `json:"by"`
	Time        int64  `json:"time"` // unix seconds
	Text        string `json:"text"` // HTML, for text posts/comments
	URL         string `json:"url"`  // external link for stories
	Title       string `json:"title"`
	Score       int    `json:"score"`
	Descendants int    `json:"descendants"` // total comment count
	Kids        []int  `json:"kids"`
	Deleted     bool   `json:"deleted"`
	Dead        bool   `json:"dead"`
}

// StoryKind selects a story list.
type StoryKind string

const (
	// Top selects the top stories list.
	Top StoryKind = "top"
	// Newest selects the newest stories list.
	//
	// NOTE: the required API named this constant "New", but that collides with
	// the New constructor function in the same package (an unavoidable Go name
	// clash), so it is exported as Newest. Its string value is still "new".
	Newest StoryKind = "new"
	// Best selects the best stories list.
	Best StoryKind = "best"
)

// get performs a GET against {BaseURL}/{path} and decodes the JSON body into v.
func (c *Client) get(ctx context.Context, path string, v any) error {
	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("hackernews: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("hackernews: GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("hackernews: GET %s: unexpected status %s", url, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("hackernews: decode %s: %w", url, err)
	}
	return nil
}

// Item fetches a single item by ID.
func (c *Client) Item(ctx context.Context, id int) (*Item, error) {
	var it Item
	if err := c.get(ctx, fmt.Sprintf("/item/%d.json", id), &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// TopStories returns the IDs of the current top stories.
func (c *Client) TopStories(ctx context.Context) ([]int, error) {
	return c.storyIDs(ctx, Top)
}

// NewStories returns the IDs of the newest stories.
func (c *Client) NewStories(ctx context.Context) ([]int, error) {
	return c.storyIDs(ctx, Newest)
}

// BestStories returns the IDs of the best stories.
func (c *Client) BestStories(ctx context.Context) ([]int, error) {
	return c.storyIDs(ctx, Best)
}

func (c *Client) storyIDs(ctx context.Context, kind StoryKind) ([]int, error) {
	var ids []int
	if err := c.get(ctx, fmt.Sprintf("/%sstories.json", kind), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// storyConcurrency bounds the number of in-flight item fetches in Stories.
const storyConcurrency = 8

// Stories fetches the first limit IDs from the given list and resolves each
// Item concurrently, preserving the list order. A limit <= 0 means all IDs.
// An error fetching any individual item aborts and is returned wrapped.
func (c *Client) Stories(ctx context.Context, kind StoryKind, limit int) ([]Item, error) {
	ids, err := c.storyIDs(ctx, kind)
	if err != nil {
		return nil, err
	}
	if limit > 0 && limit < len(ids) {
		ids = ids[:limit]
	}
	if len(ids) == 0 {
		return []Item{}, nil
	}

	items := make([]Item, len(ids))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type job struct {
		idx int
		id  int
	}
	jobs := make(chan job)

	var (
		mu       sync.Mutex
		firstErr error
	)
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}

	workers := storyConcurrency
	if workers > len(ids) {
		workers = len(ids)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				it, err := c.Item(ctx, j.id)
				if err != nil {
					setErr(fmt.Errorf("hackernews: item %d: %w", j.id, err))
					return
				}
				items[j.idx] = *it
			}
		}()
	}

	go func() {
		defer close(jobs)
		for i, id := range ids {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{idx: i, id: id}:
			}
		}
	}()

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return items, nil
}
