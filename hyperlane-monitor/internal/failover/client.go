package failover

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

// BridgeClient communicates with the Bridge service API
type BridgeClient struct {
	baseURL       string
	httpClient    *http.Client
	retryAttempts int
	retryDelay    time.Duration
}

// NewBridgeClient creates a new Bridge API client
func NewBridgeClient(baseURL string, timeout time.Duration, retryAttempts int, retryDelay time.Duration) *BridgeClient {
	return &BridgeClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retryAttempts: retryAttempts,
		retryDelay:    retryDelay,
	}
}

// TriggerFailover sends a failover request to the Bridge service
func (c *BridgeClient) TriggerFailover(ctx context.Context, request *types.FailoverRequest) (*types.FailoverResponse, error) {
	url := fmt.Sprintf("%s/api/v1/failover/trigger", c.baseURL)

	// Log the request object before marshaling
	logger.WithFields(logger.Fields{
		"message_id": request.MessageID,
		"intent_hash": request.IntentHash,
		"intent_data_nil": request.IntentData == nil,
	}).Debug("Preparing failover request")
	
	if request.IntentData != nil {
		logger.WithFields(logger.Fields{
			"intent_type": request.IntentData.IntentType,
			"symbol": request.IntentData.Symbol,
			"price": request.IntentData.Price,
			"timestamp": request.IntentData.Timestamp,
			"chainId": request.IntentData.ChainID,
			"signature_len": len(request.IntentData.Signature),
		}).Debug("Intent data details before marshaling")
	}

	// Marshal request
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// Log the marshaled JSON
	logger.WithFields(logger.Fields{
		"body_len": len(body),
		"body": string(body),
	}).Debug("Marshaled failover request JSON")

	// Try with retries
	var lastErr error
	for attempt := 0; attempt <= c.retryAttempts; attempt++ {
		if attempt > 0 {
			logger.Debugf("Retrying failover request (attempt %d/%d)", attempt, c.retryAttempts)
			time.Sleep(c.retryDelay)
		}

		response, err := c.sendRequest(ctx, url, body)
		if err == nil {
			return response, nil
		}

		lastErr = err
		logger.WithError(err).Warnf("Failover request failed (attempt %d/%d)", attempt+1, c.retryAttempts+1)
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", c.retryAttempts+1, lastErr)
}

// sendRequest sends a single HTTP request
func (c *BridgeClient) sendRequest(ctx context.Context, url string, body []byte) (*types.FailoverResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		var errorResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil && errorResp.Error != "" {
			return nil, fmt.Errorf("bridge API error (status %d): %s", resp.StatusCode, errorResp.Error)
		}
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Decode response
	var response types.FailoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// GetFailoverStatus retrieves the status of a failover request
func (c *BridgeClient) GetFailoverStatus(ctx context.Context, requestID string) (*types.FailoverResponse, error) {
	url := fmt.Sprintf("%s/api/v1/failover/status/%s", c.baseURL, requestID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response types.FailoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// CheckHealth checks if the Bridge API is accessible
func (c *BridgeClient) CheckHealth(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", c.baseURL)
	
	logger.Debugf("Checking Bridge API health at: %s (baseURL: %s)", url, c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bridge API unhealthy: status %d", resp.StatusCode)
	}

	return nil
}