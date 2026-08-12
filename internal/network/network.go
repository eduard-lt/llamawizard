package network

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// IsFree checks whether a TCP port on 127.0.0.1 is available by attempting
// a real bind and immediately closing it. This catches processes that
// `lsof` might miss (e.g. something mid-bind).
func IsFree(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// spreadOffsets defines the non-sequential offsets used to suggest
// alternative ports. Starting from a preferred port like 8080 this
// produces: 8080, 8081, 8090, 8180, 8880 — avoiding sequential ranges
// that are more likely to be congested.
var spreadOffsets = []int{0, 1, 10, 100, 800}

// SuggestAlternatives returns up to count free ports using the spread
// pattern around the preferred port. If the preferred port itself is
// free it is included as the first result.
func SuggestAlternatives(preferred int, count int) []int {
	var free []int
	for _, off := range spreadOffsets {
		if len(free) >= count {
			break
		}
		candidate := preferred + off
		if candidate < 1024 || candidate > 65535 {
			continue // skip reserved/invalid ports
		}
		if IsFree(candidate) {
			free = append(free, candidate)
		}
	}
	return free
}

// IsLlamaSwapPort checks whether the given port is serving a llama-swap
// or compatible OpenAI-completions API by probing /v1/models.
// Tries without auth first, then with the given apiKey (and "dummy" as
// fallback), since some servers require auth while others don't.
// Returns true if the endpoint responds with HTTP 200.
func IsLlamaSwapPort(port int, apiKey string) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/models", port)
	client := &http.Client{Timeout: 2 * time.Second}

	keysToTry := []string{""}
	if apiKey != "" {
		keysToTry = append(keysToTry, apiKey)
	}
	keysToTry = append(keysToTry, "dummy")

	for _, key := range keysToTry {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return true
		}
		_ = resp.Body.Close()
	}
	return false
}
