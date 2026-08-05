package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type webFetchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f webFetchRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWebFetchNetworkFailureIsRecoverable(t *testing.T) {
	fetchTool := NewWebFetchTool()
	fetchTool.client.Transport = webFetchRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Name: "weather.invalid", Err: "no such host"}
	})

	result, err := fetchTool.InvokableRun(context.Background(), `{"url":"https://weather.invalid/forecast"}`)
	if err != nil {
		t.Fatalf("network failure must not fail the tool node: %v", err)
	}
	var output WebFetchOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if output.Status != "fetch_error" || !strings.Contains(output.Message, "Do not retry") {
		t.Fatalf("unexpected recoverable output: %+v", output)
	}
}

func TestWebFetchRequestCancellationStopsTool(t *testing.T) {
	fetchTool := NewWebFetchTool()
	fetchTool.client.Transport = webFetchRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchTool.InvokableRun(ctx, `{"url":"https://example.com"}`)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v, want context.Canceled", err)
	}
}

func TestWebFetchCacheSupportsParallelResearch(t *testing.T) {
	fetchTool := NewWebFetchTool()
	fetchTool.client.Transport = webFetchRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("<title>Source</title><p>verified content</p>")),
			Request:    req,
		}, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := fetchTool.InvokableRun(context.Background(), `{"url":"https://example.com/research"}`); err != nil {
				t.Errorf("parallel fetch failed: %v", err)
			}
		}()
	}
	wg.Wait()
}
