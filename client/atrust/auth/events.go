package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Event struct {
	Name string `json:"event"`
	Data struct {
		Type string `json:"type"`
	} `json:"data"`
}

// Events reads the deployed gateway's stream using the existing cookie jar.
// An empty batch makes no assertion about past logout reasons.
func (s *Session) Events(ctx context.Context, cursor string) ([]Event, string, error) {
	if cursor == "" {
		cursor = "0"
	}
	params := WithSharedParams(url.Values{"timeout": {"20000"}, "fromId": {cursor}})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/controller/v1/public/events?"+params.Encode(), nil)
	if err != nil {
		return nil, cursor, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("x-csrf-token", s.csrfToken)
	req.Header.Set("x-sdp-traceid", s.randSdpId())
	s.client.Timeout = 25 * time.Second
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, cursor, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, cursor, fmt.Errorf("event endpoint HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Events []Event `json:"events"`
			ID     string  `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, cursor, err
	}
	if payload.Code != 0 {
		return nil, cursor, fmt.Errorf("event endpoint code %d", payload.Code)
	}
	if payload.Data.ID != "" {
		cursor = payload.Data.ID
	}
	return payload.Data.Events, cursor, nil
}
