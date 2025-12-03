package transaction

import (
	"math/big"
	"testing"

	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

func TestClient_BuildParams_EnrichmentFullIntent(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	client := &Client{
		queueManager: qm,
		chainID:      1,
	}

	methodConfig := &config.DestinationMethodConfig{
		Name: "handleIntentUpdate",
		ABI:  `{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple","components":[{"name":"intentType","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"nonce","type":"uint256"},{"name":"expiry","type":"uint256"},{"name":"symbol","type":"string"},{"name":"price","type":"uint256"},{"name":"timestamp","type":"uint256"},{"name":"source","type":"string"},{"name":"signature","type":"bytes"},{"name":"signer","type":"address"}]}]}`,
		Params: map[string]string{
			"intent": "${enrichment.fullIntent}",
		},
	}

	intent := &bridgetypes.OracleIntent{
		IntentType: "oracle_price",
		Version:    "1.0",
		ChainID:    big.NewInt(1),
		Nonce:      big.NewInt(100),
		Expiry:     big.NewInt(1000000),
		Symbol:     "BTC",
		Price:      big.NewInt(50000),
		Timestamp:  big.NewInt(1234567890),
		Source:     "test-source",
		Signature:  []byte("signature"),
	}

	updateReq := &bridgetypes.UpdateRequest{
		ExtractedData: &config.ExtractedData{
			Enrichment: map[string]interface{}{
				"fullIntent": intent,
			},
		},
	}

	params, err := client.BuildParams(methodConfig, updateReq)
	if err != nil {
		t.Errorf("BuildParams failed: %v", err)
	}

	if len(params) != 1 {
		t.Errorf("Expected 1 parameter, got %d", len(params))
	}
}

func TestClient_BuildParams_EventRequestId(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	client := &Client{
		queueManager: qm,
		chainID:      1,
	}

	methodConfig := &config.DestinationMethodConfig{
		Name: "handleRandomness",
		ABI:  `{"name":"handleRandomness","type":"function","inputs":[{"name":"requestId","type":"uint256"}]}`,
		Params: map[string]string{
			"requestId": "${event.requestId}",
		},
	}

	requestId := big.NewInt(12345)
	updateReq := &bridgetypes.UpdateRequest{
		Event: &bridgetypes.EventData{
			RequestId: requestId,
		},
	}

	params, err := client.BuildParams(methodConfig, updateReq)
	if err != nil {
		t.Errorf("BuildParams failed: %v", err)
	}

	if len(params) != 1 {
		t.Errorf("Expected 1 parameter, got %d", len(params))
	}

	if paramRequestId, ok := params[0].(*big.Int); !ok {
		t.Error("Parameter should be *big.Int")
	} else if paramRequestId.Cmp(requestId) != 0 {
		t.Errorf("Expected requestId %s, got %s", requestId.String(), paramRequestId.String())
	}
}

func TestClient_BuildParams_MissingEnrichment(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	client := &Client{
		queueManager: qm,
		chainID:      1,
	}

	methodConfig := &config.DestinationMethodConfig{
		Name: "handleIntentUpdate",
		ABI:  `{"name":"handleIntentUpdate","type":"function","inputs":[{"name":"intent","type":"tuple","components":[{"name":"symbol","type":"string"}]}]}`,
		Params: map[string]string{
			"intent": "${enrichment.fullIntent}",
		},
	}

	updateReq := &bridgetypes.UpdateRequest{
		ExtractedData: &config.ExtractedData{
			Enrichment: map[string]interface{}{},
		},
	}

	_, err := client.BuildParams(methodConfig, updateReq)
	if err == nil {
		t.Error("BuildParams should fail when enrichment is missing")
	}
}

func TestClient_BuildParams_InvalidABI(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	client := &Client{
		queueManager: qm,
		chainID:      1,
	}

	methodConfig := &config.DestinationMethodConfig{
		Name:   "invalidMethod",
		ABI:    `invalid json`,
		Params: map[string]string{},
	}

	updateReq := &bridgetypes.UpdateRequest{}

	_, err := client.BuildParams(methodConfig, updateReq)
	if err == nil {
		t.Error("BuildParams should fail with invalid ABI")
	}
}

func TestClient_ResolveParameterValue_LiteralValue(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	client := &Client{
		queueManager: qm,
		chainID:      1,
	}

	updateReq := &bridgetypes.UpdateRequest{}

	value, err := client.resolveParameterValue("literal_value", updateReq)
	if err != nil {
		t.Errorf("resolveParameterValue failed: %v", err)
	}

	if str, ok := value.(string); !ok {
		t.Error("Value should be string")
	} else if str != "literal_value" {
		t.Errorf("Expected 'literal_value', got '%s'", str)
	}
}

func TestClient_ResolveParameterValue_UnsupportedVariable(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	client := &Client{
		queueManager: qm,
		chainID:      1,
	}

	updateReq := &bridgetypes.UpdateRequest{}

	_, err := client.resolveParameterValue("${unsupported.variable}", updateReq)
	if err == nil {
		t.Error("resolveParameterValue should fail with unsupported variable")
	}
}

func TestClient_VerifyQueueManagerIntegration(t *testing.T) {
	qm := NewQueueManager(10, nil)
	qm.Start()
	defer qm.Stop()

	walletAddr := "0x1234567890123456789012345678901234567890"
	chainID := int64(1)

	_ = &Client{
		queueManager: qm,
		walletAddr:   walletAddr,
		chainID:      chainID,
	}

	stats := qm.GetQueueStats()
	if len(stats) != 0 {
		t.Error("Should have no queues initially")
	}
}
