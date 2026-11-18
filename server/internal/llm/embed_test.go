package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestEmbedOrderHeadersAndErrors(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		bad    bool
	}{
		{`{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}],"usage":{"prompt_tokens":7}}`, 200, false},
		{`{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`, 200, true},
		{`{"error":{"message":"quota"}}`, 429, true},
		{`no json`, 200, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") == "" {
					t.Errorf("request %s %v", r.URL, r.Header)
				}
				var in struct {
					Model string
					Input []string
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Model != "bge-m3" || len(in.Input) != 2 {
					t.Error(in, err)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			v, u, err := c.Embed(t.Context(), "bge-m3", []string{"a", "b"})
			if tc.bad {
				var ae *APIError
				if !errors.As(err, &ae) {
					t.Fatalf("want APIError: %v", err)
				}
				return
			}
			if err != nil || len(v) != 2 || v[0][0] != 1 || v[1][0] != 3 || u.PromptTokens != 7 {
				t.Fatal(v, u, err)
			}
		})
	}
}
func TestEmbeddingSuggestions(t *testing.T) {
	for _, id := range []string{"baai/bge-m3", "gte-large", "e5-large", "m3e-base", "text-embedding-3-small"} {
		s := Suggest(KindCustom, UpstreamModel{ID: id})
		if s.Kind != "embedding" || s.Chat || s.Image || s.Tools {
			t.Fatal(id, s)
		}
	}
}
