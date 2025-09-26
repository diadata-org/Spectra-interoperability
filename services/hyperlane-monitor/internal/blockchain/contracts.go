package blockchain

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Contract ABIs (simplified versions for the methods we need)
const (
	// OracleTrigger MessageDispatched event ABI
	MessageDispatchedEventABI = `[{
		"name": "MessageDispatched",
		"type": "event",
		"inputs": [
			{"name": "chainId", "type": "uint32", "indexed": false},
			{"name": "recipientAddress", "type": "address", "indexed": false},
			{"name": "messageId", "type": "bytes32", "indexed": true},
			{"name": "intentHash", "type": "bytes32", "indexed": false},
			{"name": "symbol", "type": "string", "indexed": false}
		]
	}]`

	// OracleIntentRegistry getIntent method ABI
	OracleIntentRegistryABI = `[{
		"name": "getIntent",
		"type": "function",
		"inputs": [{"name": "intentHash", "type": "bytes32"}],
		"outputs": [{
			"name": "intent",
			"type": "tuple",
			"components": [
				{"name": "intentType", "type": "string"},
				{"name": "version", "type": "string"},
				{"name": "chainId", "type": "uint256"},
				{"name": "nonce", "type": "uint256"},
				{"name": "expiry", "type": "uint256"},
				{"name": "symbol", "type": "string"},
				{"name": "price", "type": "uint256"},
				{"name": "timestamp", "type": "uint256"},
				{"name": "source", "type": "string"},
				{"name": "signature", "type": "bytes"},
				{"name": "signer", "type": "address"}
			]
		}]
	}]`

	// PushOracleReceiver isProcessedIntent method ABI
	PushOracleReceiverABI = `[{
		"name": "isProcessedIntent",
		"type": "function",
		"inputs": [{"name": "_intentHash", "type": "bytes32"}],
		"outputs": [{"name": "", "type": "bool"}],
		"stateMutability": "view"
	}]`
)

// MessageDispatchedEvent represents the MessageDispatched event
type MessageDispatchedEvent struct {
	ChainId          uint32
	RecipientAddress common.Address
	MessageId        common.Hash
	IntentHash       common.Hash
	Symbol           string
	Raw              LogData
}

// LogData contains raw log information
type LogData struct {
	BlockNumber uint64
	TxHash      common.Hash
	LogIndex    uint
}

// ParsedABIs holds parsed contract ABIs
type ParsedABIs struct {
	OracleTrigger      abi.ABI
	OracleRegistry     abi.ABI
	PushOracleReceiver abi.ABI
}

// ParseABIs parses all required contract ABIs
func ParseABIs() (*ParsedABIs, error) {
	triggerABI, err := abi.JSON(strings.NewReader(MessageDispatchedEventABI))
	if err != nil {
		return nil, err
	}

	registryABI, err := abi.JSON(strings.NewReader(OracleIntentRegistryABI))
	if err != nil {
		return nil, err
	}

	receiverABI, err := abi.JSON(strings.NewReader(PushOracleReceiverABI))
	if err != nil {
		return nil, err
	}

	return &ParsedABIs{
		OracleTrigger:      triggerABI,
		OracleRegistry:     registryABI,
		PushOracleReceiver: receiverABI,
	}, nil
}

// OracleIntent matches the contract struct
type OracleIntent struct {
	IntentType string
	Version    string
	ChainId    *big.Int
	Nonce      *big.Int
	Expiry     *big.Int
	Symbol     string
	Price      *big.Int
	Timestamp  *big.Int
	Source     string
	Signature  []byte
	Signer     common.Address
}