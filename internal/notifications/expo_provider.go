package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const expoPushURL = "https://exp.host/--/api/v2/push/send"

type ExpoProvider struct {
	client *http.Client
}

func NewExpoProvider(client *http.Client) *ExpoProvider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &ExpoProvider{client: client}
}

func (p *ExpoProvider) Send(ctx context.Context, message PushMessage) (*PushResult, error) {
	payloadBytes, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, expoPushURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("expo push failed: status=%d body=%s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseExpoPushResult(body)
}

type expoPushEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type expoPushTicket struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

func parseExpoPushResult(body []byte) (*PushResult, error) {
	var envelope expoPushEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}

	ticket, err := parseExpoPushTicket(envelope.Data)
	if err != nil {
		return nil, err
	}

	if ticket.Status == "error" {
		permanent := ticket.Details.Error == "DeviceNotRegistered"
		return &PushResult{
			ProviderMessageID: ticket.ID,
			PermanentFailure:  permanent,
			DisableDevice:     permanent,
		}, nil
	}

	return &PushResult{ProviderMessageID: ticket.ID}, nil
}

func parseExpoPushTicket(raw json.RawMessage) (*expoPushTicket, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("expo push response missing ticket data")
	}

	var single expoPushTicket
	if err := json.Unmarshal(raw, &single); err == nil && (single.Status != "" || single.ID != "" || single.Message != "") {
		return &single, nil
	}

	var many []expoPushTicket
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, fmt.Errorf("expo push response returned no tickets")
	}
	return &many[0], nil
}
