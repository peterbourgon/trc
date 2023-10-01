# trc [![Go Reference](https://pkg.go.dev/badge/github.com/peterbourgon/trc.svg)](https://pkg.go.dev/github.com/peterbourgon/trc) ![Latest Release](https://img.shields.io/github/v/release/peterbourgon/trc?style=flat-square) ![Build Status](https://github.com/peterbourgon/trc/actions/workflows/test.yaml/badge.svg?branch=main)

trc provides in-process request tracing, an efficient alternative to logging.
It's heavily inspired by [x/net/trace](https://golang.org/x/net/trace), much
gratitude to those authors.

# Demo

```shell
cd _examples/trc-complex
go install
trc-complex
# open http://localhost:8080/traces in your browser
```

# Usage

Most users should import and use [package eztrc][eztrc] as it solves normal use
cases. Here's an example of how you might wire up tracing to a typical HTTP
server.

[eztrc]: https://pkg.go.dev/github.com/peterbourgon/trc/eztrc

```go
func main() {
	var appHandler http.Handler
	appHandler = NewAppServer(...)
	appHandler = eztrc.Middleware(categorize)(appHandler)
	go func() { log.Fatal(http.ListenAndServe(":8080", appHandler)) }()

	var trcHandler http.Handler
	trcHandler = eztrc.Handler()
	go func() { log.Fatal(http.ListenAndServe(":8081", trcHandler)) }()

	select {}
}

func categorize(r *http.Request) string {
	return r.Method + " " + r.URL.Path // assuming a finite set of possible paths
}
```

Your code then "logs" to the trace in the context.

```go
func someFunction(ctx context.Context, ...) {
	eztrc.Tracef(ctx, "log statement") // directly thru the context

	tr := eztrc.Get(ctx)          // extract the trace from the context
	tr.Errorf("this is an error") // call methods on the trace value
```

Traces can be viewed, queried, etc. through a web UI served by the trc handler.

<kbd><img src="/ui.png"/></kbd>

# Errata

The module is currently experimental and unstable. Breaking changes are
guaranteed. Use at your own risk.
