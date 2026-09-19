package rag

import (
	"context"
	"net"
	"net/http"
	"time"
)

// testCtx is the context the corpus tests run the pipeline under.
func testCtx() context.Context {
	return context.Background()
}

// noNetClient is an HTTP client that cannot reach any network: its dialer
// fails instantly on every connection. The extractive path makes no calls,
// so a test using this client proves the answer it produced required no
// third-party service — which is the offline guarantee the pitch demo
// rests on. If a code change ever makes extraction phone home, these
// tests fail loudly instead of silently depending on the internet.
func noNetClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, noNetworkError{}
			},
		},
		Timeout: 100 * time.Millisecond,
	}
}

type noNetworkError struct{}

func (noNetworkError) Error() string { return "test: network access is not allowed here" }
