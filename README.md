# trc [![Go Reference](https://pkg.go.dev/badge/github.com/peterbourgon/trc.svg)](https://pkg.go.dev/github.com/peterbourgon/trc) ![Latest Release](https://img.shields.io/github/v/release/peterbourgon/trc?style=flat-square) ![Build Status](https://github.com/peterbourgon/trc/actions/workflows/test.yml/badge.svg?branch=main)

trc provides in-process request tracing, an efficient alternative to logging.

<kbd><img src="/ui.png"/></kbd>

Most consumers should import [package eztrc][eztrc], which provides an API
specifically designed for common use cases.

[eztrc]: https://pkg.go.dev/github.com/peterbourgon/trc/eztrc

Here's a quick-and-dirty example for a typical HTTP server.

```go
func main() {
	server := NewServer(...) // your HTTP server
	traced := eztrc.Middleware(categorize)(server)
	go func() { log.Fatal(http.ListenAndServe(":8080", traced)) }() // normal API

	traces := eztrc.Handler()
	go func() { log.Fatal(http.ListenAndServe(":8081", traces)) }() // traces UI

	select {}
}

func categorize(r *http.Request) string {
	return r.Method + " " + r.URL.Path // assuming a fixed and finite set of possible paths
}

func someFunction(ctx context.Context, ...) {
	eztrc.Tracef(ctx, "this is a log statement")
	// ...
}
```

See the [examples dir](https://github.com/peterbourgon/trc/tree/main/_examples)
for more complete example applications.

The current API is experimental and unstable. Breaking changes are guaranteed.
Use at your own risk.
