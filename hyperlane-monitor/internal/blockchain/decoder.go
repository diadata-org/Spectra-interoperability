package blockchain

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

// HyperlaneMessageDecoder decodes Hyperlane message bodies
type HyperlaneMessageDecoder struct {
	// Cache the intent struct type for decoding
	intentType abi.Type
}

// NewHyperlaneMessageDecoder creates a new message decoder
func NewHyperlaneMessageDecoder() (*HyperlaneMessageDecoder, error) {
	// Define the OracleIntent struct type for ABI decoding
	intentComponents := []abi.ArgumentMarshaling{
		{Name: "intentType", Type: "string"},
		{Name: "version", Type: "string"},
		{Name: "chainId", Type: "uint256"},
		{Name: "nonce", Type: "uint256"},
		{Name: "expiry", Type: "uint256"},
		{Name: "symbol", Type: "string"},
		{Name: "price", Type: "uint256"},
		{Name: "timestamp", Type: "uint256"},
		{Name: "source", Type: "string"},
		{Name: "signature", Type: "bytes"},
		{Name: "signer", Type: "address"},
	}

	intentType, err := abi.NewType("tuple", "", intentComponents)
	if err != nil {
		return nil, fmt.Errorf("failed to create intent type: %w", err)
	}

	return &HyperlaneMessageDecoder{
		intentType: intentType,
	}, nil
}

// DecodeMessageBody decodes a Hyperlane message body containing an OracleIntent
func (d *HyperlaneMessageDecoder) DecodeMessageBody(messageBody []byte) (*types.OracleIntent, error) {
	// The message body from OracleTrigger contains the encoded OracleIntent
	// It's ABI encoded as a single tuple parameter
	
	// Create the arguments for unpacking
	args := abi.Arguments{
		{Type: d.intentType},
	}

	// Unpack the data
	unpacked, err := args.Unpack(messageBody)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack message body: %w", err)
	}

	if len(unpacked) != 1 {
		return nil, fmt.Errorf("unexpected number of unpacked values: %d", len(unpacked))
	}

	// The unpacked value is an interface{} containing the struct fields
	intentData, ok := unpacked[0].(struct {
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
	})
	if !ok {
		return nil, fmt.Errorf("failed to cast unpacked data to intent struct")
	}

	// Convert to our types.OracleIntent
	intent := &types.OracleIntent{
		IntentType: intentData.IntentType,
		Version:    intentData.Version,
		ChainID:    intentData.ChainId,
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

// CalculateIntentHash calculates the EIP-712 hash for an OracleIntent
func CalculateIntentHash(intent *types.OracleIntent) (common.Hash, error) {
	// EIP-712 Domain Separator (must match PushOracleReceiver)
	domainSeparator := crypto.Keccak256Hash(
		crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract,bytes32 salt)")),
		crypto.Keccak256([]byte("DIA Oracle Intent")),
		crypto.Keccak256([]byte("1")),
		common.LeftPadBytes(big.NewInt(100640).Bytes(), 32), // DIA testnet chainId
		common.HexToAddress("0xd2313dcabB0E9447d800546b953E05dD47EB2eB9").Bytes(), // OracleIntentRegistry
		make([]byte, 32), // salt (zero)
	)

	// Intent Type Hash
	intentTypeHash := crypto.Keccak256([]byte(
		"OracleIntent(string intentType,string version,uint256 chainId,uint256 nonce,uint256 expiry,string symbol,uint256 price,uint256 timestamp,string source)",
	))

	// Struct Hash
	structHash := crypto.Keccak256(
		intentTypeHash,
		crypto.Keccak256([]byte(intent.IntentType)),
		crypto.Keccak256([]byte(intent.Version)),
		common.LeftPadBytes(intent.ChainID.Bytes(), 32),
		common.LeftPadBytes(intent.Nonce.Bytes(), 32),
		common.LeftPadBytes(intent.Expiry.Bytes(), 32),
		crypto.Keccak256([]byte(intent.Symbol)),
		common.LeftPadBytes(intent.Price.Bytes(), 32),
		common.LeftPadBytes(intent.Timestamp.Bytes(), 32),
		crypto.Keccak256([]byte(intent.Source)),
	)

	// Final EIP-712 hash
	finalHash := crypto.Keccak256(
		[]byte("\x19\x01"),
		domainSeparator.Bytes(),
		structHash,
	)

	return common.BytesToHash(finalHash), nil
}

// ExtractIntentHashFromMessage extracts the intent hash from a decoded Hyperlane message
func ExtractIntentHashFromMessage(messageBody []byte) (common.Hash, error) {
	decoder, err := NewHyperlaneMessageDecoder()
	if err != nil {
		return common.Hash{}, err
	}

	intent, err := decoder.DecodeMessageBody(messageBody)
	if err != nil {
		return common.Hash{}, err
	}

	return CalculateIntentHash(intent)
}

// DecodeHexString decodes a hex string (with or without 0x prefix)
func DecodeHexString(hexStr string) ([]byte, error) {
	// Remove 0x prefix if present
	if len(hexStr) >= 2 && hexStr[0:2] == "0x" {
		hexStr = hexStr[2:]
	}
	
	return hex.DecodeString(hexStr)
}