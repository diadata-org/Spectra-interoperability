package transaction

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/diadata.org/Spectra-interoperability/pkg/logger"
	"github.com/diadata.org/Spectra-interoperability/pkg/rpc"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/config"
	"github.com/diadata.org/Spectra-interoperability/services/bridge/internal/contracts"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

type Client struct {
	executor     *Executor
	queueManager *QueueManager
	walletAddr   string
	chainID      int64
}

func NewClient(receiverClient *contracts.ReceiverClient, ethClient rpc.EthClient, queueManager *QueueManager, chainID int64) *Client {
	executor := NewExecutor(receiverClient, ethClient, chainID)
	walletAddr := receiverClient.GetAuth().From.Hex()

	return &Client{
		executor:     executor,
		queueManager: queueManager,
		walletAddr:   walletAddr,
		chainID:      chainID,
	}
}

func (c *Client) CallMethod(ctx context.Context, contractAddr, methodName, abiJSON string, params []interface{}, gasPrice *big.Int, gasLimit uint64, updateReq *bridgetypes.UpdateRequest) (*types.Transaction, error) {
	queue, err := c.queueManager.GetOrCreateQueue(c.walletAddr, c.chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction queue: %w", err)
	}

	req := &Request{
		Ctx:           ctx,
		ContractAddr:  contractAddr,
		MethodName:    methodName,
		ABI:           abiJSON,
		Params:        params,
		GasPrice:      gasPrice,
		GasLimit:      gasLimit,
		UpdateRequest: updateReq,
	}

	executorFunc := func(execCtx context.Context) (*types.Transaction, error) {
		return c.executor.Execute(execCtx, req)
	}

	// Extract metadata for queue visibility
	meta := SubmitMeta{
		ChainID:  c.chainID,
	}
	if updateReq != nil {
		meta.RouterID = updateReq.RouterID
		meta.Contract = contractAddr
		if updateReq.Intent != nil {
			meta.Symbol = updateReq.Intent.Symbol
			if updateReq.Intent.Timestamp != nil {
				meta.OnchainTS = updateReq.Intent.Timestamp.Int64()
			}
		}
	}

	t0 := time.Now()
	tx, err := queue.Submit(ctx, executorFunc, meta)
	logger.Infof("[QUEUE-SUBMIT] CallMethod %s on %s: took=%v", methodName, contractAddr, time.Since(t0))
	return tx, err
}

func (c *Client) BuildParams(methodConfig *config.DestinationMethodConfig, updateReq *bridgetypes.UpdateRequest) ([]interface{}, error) {
	logger.Infof("[BUILD-PARAMS] Building params for method: %s", methodConfig.Name)

	parsedABI, err := abi.JSON(strings.NewReader(fmt.Sprintf(`[%s]`, methodConfig.ABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse method ABI: %w", err)
	}

	method, exists := parsedABI.Methods[methodConfig.Name]
	if !exists {
		return nil, fmt.Errorf("method %s not found in ABI", methodConfig.Name)
	}

	params := make([]interface{}, len(method.Inputs))
	for i, input := range method.Inputs {
		paramName := input.Name
		paramSource, exists := methodConfig.Params[paramName]
		if !exists {
			return nil, fmt.Errorf("parameter %s (position %d) not found in config", paramName, i)
		}

		logger.Infof("[BUILD-PARAMS] [%d] Resolving param: %s from source: %s", i, paramName, paramSource)

		value, err := c.resolveParameterValue(paramSource, updateReq)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve parameter %s: %w", paramName, err)
		}

		logger.Infof("[BUILD-PARAMS] [%d] Resolved param %s: Type=%T", i, paramName, value)

		switch v := value.(type) {
		case []*big.Int:
			logger.Infof("[BUILD-PARAMS] [%d]   %s is []*big.Int with %d elements", i, paramName, len(v))
		case []interface{}:
			logger.Infof("[BUILD-PARAMS] [%d]   %s is []interface{} with %d elements - NEEDS CONVERSION", i, paramName, len(v))
		}

		if paramName == "intent" && paramSource == "${enrichment.fullIntent}" {
			if intent, ok := value.(*bridgetypes.OracleIntent); ok {
				tuple := struct {
					IntentType string         `abi:"intentType"`
					Version    string         `abi:"version"`
					ChainId    *big.Int       `abi:"chainId"`
					Nonce      *big.Int       `abi:"nonce"`
					Expiry     *big.Int       `abi:"expiry"`
					Symbol     string         `abi:"symbol"`
					Price      *big.Int       `abi:"price"`
					Timestamp  *big.Int       `abi:"timestamp"`
					Source     string         `abi:"source"`
					Signature  []byte         `abi:"signature"`
					Signer     common.Address `abi:"signer"`
				}{
					IntentType: intent.IntentType,
					Version:    intent.Version,
					ChainId:    intent.ChainID,
					Nonce:      intent.Nonce,
					Expiry:     intent.Expiry,
					Symbol:     intent.Symbol,
					Price:      intent.Price,
					Timestamp:  intent.Timestamp,
					Source:     intent.Source,
					Signature:  []byte(intent.Signature),
					Signer:     intent.Signer,
				}
				params[i] = tuple
				continue
			}
		}
		params[i] = value
	}

	logger.Infof("[BUILD-PARAMS] Built %d parameters total in ABI order", len(params))
	return params, nil
}

func (c *Client) resolveParameterValue(source string, updateReq *bridgetypes.UpdateRequest) (interface{}, error) {
	if strings.HasPrefix(source, "${") && strings.HasSuffix(source, "}") {
		templateVar := strings.TrimSuffix(strings.TrimPrefix(source, "${"), "}")

		switch {
		case strings.HasPrefix(templateVar, "enrichment."):
			enrichmentKey := strings.TrimPrefix(templateVar, "enrichment.")
			if updateReq.ExtractedData != nil && updateReq.ExtractedData.Enrichment != nil {
				if value, exists := updateReq.ExtractedData.Enrichment[enrichmentKey]; exists {
					if enrichmentKey == "fullIntent" {
						if intent, ok := value.(*bridgetypes.OracleIntent); ok {
							logger.Debugf("Retrieved fullIntent from enrichment: symbol=%s price=%s timestamp=%s nonce=%s expiry=%s signer=%s source=%s",
								intent.Symbol,
								intent.Price.String(),
								intent.Timestamp.String(),
								intent.Nonce.String(),
								intent.Expiry.String(),
								intent.Signer.Hex(),
								intent.Source)
							return intent, nil
						}
						return nil, fmt.Errorf("fullIntent has unexpected type %T", value)
					}

					return value, nil
				}
				return nil, fmt.Errorf("enrichment key %s not found", enrichmentKey)
			}
			return nil, fmt.Errorf("enrichment data not available")

		case strings.HasPrefix(templateVar, "event."):
			eventField := strings.TrimPrefix(templateVar, "event.")
			if updateReq.Event == nil {
				return nil, fmt.Errorf("event data not available")
			}

			switch eventField {
			case "requestId":
				if updateReq.Event.RequestId != nil {
					return updateReq.Event.RequestId, nil
				}
				return nil, fmt.Errorf("event requestId not found")
			default:
				return nil, fmt.Errorf("unsupported event field: %s", eventField)
			}

		case strings.HasPrefix(templateVar, "intent."):
			if updateReq.Intent == nil {
				return nil, fmt.Errorf("intent data not available")
			}
			return updateReq.Intent, nil

		default:
			return nil, fmt.Errorf("unsupported template variable: %s", templateVar)
		}
	}

	return source, nil
}
