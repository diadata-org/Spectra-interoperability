package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/logger"
)

type ChainClient struct {
	chainID    int
	chainName  string
	client     *ethclient.Client
	rpcClient  *rpc.Client
	abis       *ParsedABIs
}

func NewChainClient(chainID int, chainName string, rpcURLs []string) (*ChainClient, error) {
	var client *ethclient.Client
	var rpcClient *rpc.Client
	var err error

	for _, url := range rpcURLs {
		rpcClient, err = rpc.DialContext(context.Background(), url)
		if err != nil {
			logger.Warnf("Failed to connect to %s: %v", url, err)
			continue
		}

		client = ethclient.NewClient(rpcClient)
		
		_, err = client.ChainID(context.Background())
		if err != nil {
			logger.Warnf("Failed to get chain ID from %s: %v", url, err)
			rpcClient.Close()
			continue
		}

		logger.Infof("Connected to %s chain via %s", chainName, url)
		break
	}

	if client == nil {
		return nil, fmt.Errorf("failed to connect to any RPC endpoint for chain %d", chainID)
	}

	abis, err := ParseABIs()
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABIs: %w", err)
	}

	return &ChainClient{
		chainID:   chainID,
		chainName: chainName,
		client:    client,
		rpcClient: rpcClient,
		abis:      abis,
	}, nil
}

func (c *ChainClient) Close() {
	if c.rpcClient != nil {
		c.rpcClient.Close()
	}
}
func (c *ChainClient) GetLatestBlock(ctx context.Context) (uint64, error) {
	return c.client.BlockNumber(ctx)
}

// FilterMessageDispatchedEvents filters for MessageDispatched events
func (c *ChainClient) FilterMessageDispatchedEvents(ctx context.Context, triggerAddr common.Address, fromBlock, toBlock uint64) ([]MessageDispatchedEvent, error) {
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Addresses: []common.Address{triggerAddr},
		Topics: [][]common.Hash{
			{c.abis.OracleTrigger.Events["MessageDispatched"].ID},
		},
	}

	logs, err := c.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to filter logs: %w", err)
	}

	var events []MessageDispatchedEvent
	for _, vLog := range logs {
		event := MessageDispatchedEvent{
			Raw: LogData{
				BlockNumber: vLog.BlockNumber,
				TxHash:      vLog.TxHash,
				LogIndex:    vLog.Index,
			},
		}

		// Parse the event
		err := c.abis.OracleTrigger.UnpackIntoInterface(&event, "MessageDispatched", vLog.Data)
		if err != nil {
			logger.Errorf("Failed to unpack MessageDispatched event: %v", err)
			continue
		}

		// MessageId is in topics[1]
		if len(vLog.Topics) > 1 {
			event.MessageId = vLog.Topics[1]
		}

		events = append(events, event)
	}

	return events, nil
}

// GetOracleIntent calls the getIntent method on OracleIntentRegistry
func (c *ChainClient) GetOracleIntent(ctx context.Context, registryAddr common.Address, intentHash common.Hash) (*OracleIntent, error) {
	// Create a call message
	callData, err := c.abis.OracleRegistry.Pack("getIntent", intentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to pack getIntent call: %w", err)
	}

	msg := ethereum.CallMsg{
		To:   &registryAddr,
		Data: callData,
	}

	// Call the contract
	output, err := c.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call getIntent: %w", err)
	}

	// Unpack the result
	// The output is a struct returned as a single element
	results, err := c.abis.OracleRegistry.Unpack("getIntent", output)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getIntent result: %w", err)
	}
	
	if len(results) == 0 {
		return nil, fmt.Errorf("no results from getIntent")
	}
	
	// The result should be a struct
	intentData, ok := results[0].(struct {
		IntentType string         `json:"intentType"`
		Version    string         `json:"version"`
		ChainId    *big.Int       `json:"chainId"`
		Nonce      *big.Int       `json:"nonce"`
		Expiry     *big.Int       `json:"expiry"`
		Symbol     string         `json:"symbol"`
		Price      *big.Int       `json:"price"`
		Timestamp  *big.Int       `json:"timestamp"`
		Source     string         `json:"source"`
		Signature  []byte         `json:"signature"`
		Signer     common.Address `json:"signer"`
	})
	if !ok {
		return nil, fmt.Errorf("unexpected result type from getIntent")
	}
	
	// Convert to our OracleIntent type
	intent := &OracleIntent{
		IntentType: intentData.IntentType,
		Version:    intentData.Version,
		ChainId:    intentData.ChainId,
		Nonce:      intentData.Nonce,
		Expiry:     intentData.Expiry,
		Symbol:     intentData.Symbol,
		Price:      intentData.Price,
		Timestamp:  intentData.Timestamp,
		Source:     intentData.Source,
		Signature:  intentData.Signature,
		Signer:     intentData.Signer,
	}

	return intent, nil
}

// IsIntentProcessed checks if an intent has been processed on PushOracleReceiver
func (c *ChainClient) IsIntentProcessed(ctx context.Context, receiverAddr common.Address, intentHash common.Hash) (bool, error) {
	// Create a call message
	callData, err := c.abis.PushOracleReceiver.Pack("isProcessedIntent", intentHash)
	if err != nil {
		return false, fmt.Errorf("failed to pack isProcessedIntent call: %w", err)
	}

	msg := ethereum.CallMsg{
		To:   &receiverAddr,
		Data: callData,
	}

	// Call the contract
	output, err := c.client.CallContract(ctx, msg, nil)
	if err != nil {
		return false, fmt.Errorf("failed to call isProcessedIntent: %w", err)
	}

	// Unpack the result
	var processed bool
	err = c.abis.PushOracleReceiver.UnpackIntoInterface(&processed, "isProcessedIntent", output)
	if err != nil {
		return false, fmt.Errorf("failed to unpack isProcessedIntent result: %w", err)
	}

	return processed, nil
}

// GetTransaction retrieves a transaction by hash
func (c *ChainClient) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	return c.client.TransactionByHash(ctx, hash)
}

// WaitForTransaction waits for a transaction to be mined
func (c *ChainClient) WaitForTransaction(ctx context.Context, hash common.Hash, timeout time.Duration) (*types.Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for transaction %s", hash.Hex())
		case <-ticker.C:
			receipt, err := c.client.TransactionReceipt(ctx, hash)
			if err == nil {
				return receipt, nil
			}
			// Check if error is "not found"
			if err.Error() != "not found" {
				return nil, err
			}
		}
	}
}

// IsConnected checks if the client is connected to the chain
func (c *ChainClient) IsConnected() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	_, err := c.client.ChainID(ctx)
	return err == nil
}