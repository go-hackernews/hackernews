package hackernews

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNewDefaults(t *testing.T) {
	c := New()
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}
	if c.UserAgent != DefaultUserAgent {
		t.Errorf("UserAgent = %q, want %q", c.UserAgent, DefaultUserAgent)
	}
	if c.HTTPClient != http.DefaultClient {
		t.Errorf("HTTPClient = %v, want http.DefaultClient", c.HTTPClient)
	}
}

func TestOptions(t *testing.T) {
	hc := &http.Client{}
	c := New(
		WithHTTPClient(hc),
		WithBaseURL("http://example.test/v0"),
		WithUserAgent("custom-agent"),
	)
	if c.HTTPClient != hc {
		t.Error("WithHTTPClient not applied")
	}
	if c.BaseURL != "http://example.test/v0" {
		t.Errorf("WithBaseURL not applied: %q", c.BaseURL)
	}
	if c.UserAgent != "custom-agent" {
		t.Errorf("WithUserAgent not applied: %q", c.UserAgent)
	}
}

func TestItemSuccessAndUserAgent(t *testing.T) {
	want := Item{
		ID: 8863, Type: "story", By: "dhouston", Time: 1175714200,
		Title: "My YC app", URL: "http://www.getdropbox.com/u/2/screencast.html",
		Score: 111, Descendants: 71, Kids: []int{9224, 8917},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "test-ua" {
			t.Errorf("User-Agent = %q, want test-ua", got)
		}
		if r.URL.Path != "/item/8863.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithUserAgent("test-ua"))
	got, err := c.Item(context.Background(), 8863)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.Score != want.Score ||
		len(got.Kids) != 2 || got.Kids[0] != 9224 {
		t.Errorf("Item = %+v", got)
	}
}

func TestItemBuildRequestError(t *testing.T) {
	// A control character in the URL makes http.NewRequestWithContext fail.
	c := New(WithBaseURL("http://\x7f.example"))
	_, err := c.Item(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("want build request error, got %v", err)
	}
}

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("transport boom")
}

func TestItemDoError(t *testing.T) {
	c := New(
		WithBaseURL("http://example.test/v0"),
		WithHTTPClient(&http.Client{Transport: errRoundTripper{}}),
	)
	_, err := c.Item(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "transport boom") {
		t.Fatalf("want transport error, got %v", err)
	}
}

func TestItemNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Item(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("want status error, got %v", err)
	}
}

func TestItemDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "{not valid json")
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Item(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func listHandler(t *testing.T, wantPath string, ids []int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("list path = %q, want %q", r.URL.Path, wantPath)
		}
		json.NewEncoder(w).Encode(ids)
	}
}

func TestStoryListEndpoints(t *testing.T) {
	cases := []struct {
		name string
		path string
		call func(c *Client) ([]int, error)
	}{
		{"top", "/topstories.json", func(c *Client) ([]int, error) { return c.TopStories(context.Background()) }},
		{"new", "/newstories.json", func(c *Client) ([]int, error) { return c.NewStories(context.Background()) }},
		{"best", "/beststories.json", func(c *Client) ([]int, error) { return c.BestStories(context.Background()) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(listHandler(t, tc.path, []int{1, 2, 3}))
			defer srv.Close()
			c := New(WithBaseURL(srv.URL))
			ids, err := tc.call(c)
			if err != nil {
				t.Fatal(err)
			}
			if len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
				t.Errorf("ids = %v", ids)
			}
		})
	}
}

func TestStoryListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	if _, err := c.TopStories(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

// storyServer serves a list at the list endpoint and each item at /item/{id}.json.
func storyServer(t *testing.T, kind StoryKind, ids []int) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%sstories.json", kind), func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ids)
	})
	mux.HandleFunc("/item/", func(w http.ResponseWriter, r *http.Request) {
		base := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/item/"), ".json")
		id, _ := strconv.Atoi(base)
		json.NewEncoder(w).Encode(Item{ID: id, Type: "story", Title: "t" + base})
	})
	return httptest.NewServer(mux)
}

func TestStoriesOrderAndLimit(t *testing.T) {
	ids := []int{30, 10, 20, 40, 50}
	srv := storyServer(t, Top, ids)
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))

	// limit < len(ids): first three, in list order.
	items, err := c.Stories(context.Background(), Top, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("len = %d", len(items))
	}
	for i, wantID := range []int{30, 10, 20} {
		if items[i].ID != wantID {
			t.Errorf("items[%d].ID = %d, want %d", i, items[i].ID, wantID)
		}
	}
}

func TestStoriesAll(t *testing.T) {
	ids := make([]int, 20)
	for i := range ids {
		ids[i] = 100 + i
	}
	srv := storyServer(t, Newest, ids)
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))

	items, err := c.Stories(context.Background(), Newest, 0) // limit<=0 => all
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("len = %d", len(items))
	}
	for i := range items {
		if items[i].ID != 100+i {
			t.Errorf("order broken at %d: %d", i, items[i].ID)
		}
	}
}

func TestStoriesFewerThanWorkers(t *testing.T) {
	// len(ids) < storyConcurrency exercises the workers-clamp branch.
	srv := storyServer(t, Best, []int{7, 8})
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	items, err := c.Stories(context.Background(), Best, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != 7 || items[1].ID != 8 {
		t.Errorf("items = %+v", items)
	}
}

func TestStoriesEmptyList(t *testing.T) {
	srv := storyServer(t, Top, []int{})
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	items, err := c.Stories(context.Background(), Top, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("want empty, got %v", items)
	}
}

func TestStoriesListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL))
	if _, err := c.Stories(context.Background(), Top, 5); err == nil {
		t.Fatal("want list error")
	}
}

// TestStoriesItemErrorAndCancellation drives a list whose first item returns
// 500 (aborting) while every other item blocks until its request is cancelled.
// This deterministically exercises: the worker error return, the second
// setErr call (firstErr already set), and the sender's ctx.Done() branch.
func TestStoriesItemErrorAndCancellation(t *testing.T) {
	const badID = 999
	ids := []int{badID}
	for i := 0; i < 50; i++ {
		ids = append(ids, i)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/topstories.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ids)
	})
	mux.HandleFunc("/item/", func(w http.ResponseWriter, r *http.Request) {
		base := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/item/"), ".json")
		if base == strconv.Itoa(badID) {
			http.Error(w, "bad item", http.StatusInternalServerError)
			return
		}
		<-r.Context().Done() // block until the client cancels
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Stories(context.Background(), Top, 0)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("item %d", badID)) {
		t.Fatalf("want item %d error, got %v", badID, err)
	}
}
