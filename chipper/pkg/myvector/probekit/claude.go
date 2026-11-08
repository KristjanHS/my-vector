// Package probekit holds what the offline probes (searchbench, routeprobe, listenprobe, followupprobe) share.
package probekit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// Claude is one Messages API call with thinking disabled; it returns the first text block.
func Claude(key, model, system, user string, maxTokens int) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": model, "max_tokens": maxTokens, "system": system, "thinking": map[string]string{"type": "disabled"},
		"messages": []map[string]string{{"role": "user", "content": user}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, llm.AnthropicBase+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", llm.AnthropicVersion)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("judge HTTP %d", res.StatusCode)
	}
	var out struct {
		Content []struct{ Type, Text string } `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("judge reply: %v", err)
	}
	for _, c := range out.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", errors.New("judge reply has no text block")
}
