<p align="center"><img src="https://raw.githubusercontent.com/go-hackernews/brand/main/social/go-hackernews.png" alt="go-hackernews/hackernews" width="720"></p>

# hackernews

[![CI](https://github.com/go-hackernews/hackernews/actions/workflows/ci.yml/badge.svg)](https://github.com/go-hackernews/hackernews/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-hackernews/hackernews.svg)](https://pkg.go.dev/github.com/go-hackernews/hackernews)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go, dependency-free client for the official
[Hacker News Firebase API](https://github.com/HackerNews/API)
(`https://hacker-news.firebaseio.com/v0/`).

- **`CGO_ENABLED=0`** — builds a static binary on every platform.
- **Zero third-party dependencies** — standard library only
  (`net/http`, `encoding/json`, `context`, `fmt`).
- Cross-compiles to all Go 64-bit targets
  (linux/{amd64,arm64,riscv64,ppc64le,s390x,loong64}, darwin/{amd64,arm64}, windows/amd64).
- 100% test coverage, network-free (`net/http/httptest`).

## Install

```sh
go get github.com/go-hackernews/hackernews
```

Requires Go 1.26.4 or newer.

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-hackernews/hackernews"
)

func main() {
	ctx := context.Background()
	c := hackernews.New()

	// Fetch a single item.
	item, err := c.Item(ctx, 8863)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s by %s (%d points)\n", item.Title, item.By, item.Score)

	// Fetch the top 10 stories, fully resolved and in list order.
	stories, err := c.Stories(ctx, hackernews.Top, 10)
	if err != nil {
		log.Fatal(err)
	}
	for i, s := range stories {
		fmt.Printf("%2d. %s\n", i+1, s.Title)
	}

	// Just the ID lists are available too.
	ids, err := c.TopStories(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d top-story IDs\n", len(ids))
}
```

### Options

`New` accepts functional options:

```go
c := hackernews.New(
	hackernews.WithHTTPClient(myClient),
	hackernews.WithBaseURL("http://127.0.0.1:8080/v0"),
	hackernews.WithUserAgent("my-app/1.0"),
)
```

### Story kinds

`Stories` takes a `StoryKind`:

| Constant             | List endpoint          |
| -------------------- | ---------------------- |
| `hackernews.Top`     | `/topstories.json`     |
| `hackernews.Newest`  | `/newstories.json`     |
| `hackernews.Best`    | `/beststories.json`    |

> **Note:** the newest-stories constant is named `Newest` (not `New`) because
> `New` is the package's constructor function; Go does not allow a function and
> a constant to share a name in the same package.

`Stories` fetches the first `limit` IDs from the chosen list (a `limit <= 0`
means all of them) and resolves each `Item` concurrently, using a bounded pool
of goroutines while preserving the original list order. An error on any single
item aborts the batch and is returned wrapped, and context cancellation is
propagated to all in-flight requests.

## License

[BSD-3-Clause](LICENSE) — Copyright (c) the go-hackernews/hackernews authors.
