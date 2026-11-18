package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
)

// Embed returns vectors in input order, regardless of upstream response order.
func (c *Client) Embed(ctx context.Context, model string, inputs []string) ([][]float32, *Usage, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil, nil
	}
	body, err := json.Marshal(map[string]any{"model": model, "input": inputs})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint(c.BaseURL, "/embeddings"), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, nil, &APIError{Status: resp.StatusCode, Message: extractError(raw)}
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage *Usage `json:"usage"`
	}
	bad := func(msg string) ([][]float32, *Usage, error) {
		return nil, out.Usage, &APIError{Status: 502, Message: msg}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return bad("invalid embeddings response: " + err.Error())
	}
	if len(out.Data) != len(inputs) {
		return bad("wrong embedding count")
	}
	vectors := make([][]float32, len(inputs))
	dim := 0
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(inputs) || vectors[d.Index] != nil || len(d.Embedding) == 0 {
			return bad("invalid embedding index or vector")
		}
		if dim == 0 {
			dim = len(d.Embedding)
		}
		if len(d.Embedding) != dim {
			return bad("inconsistent embedding dimensions")
		}
		for _, v := range d.Embedding {
			if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
				return bad("non-finite embedding")
			}
		}
		vectors[d.Index] = d.Embedding
	}
	return vectors, out.Usage, nil
}
