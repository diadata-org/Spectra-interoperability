package main

import (
	"context"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRPCClient is a mock implementation of RPCClient interface
type MockRPCClient struct {
	mock.Mock
}

func (m *MockRPCClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	callArgs := m.Called(ctx, result, method, args)
	// Copy the mocked result into the actual result interface{}
	if res, ok := callArgs.Get(0).([]byte); ok {
		// Assuming result is a *string for eth_call results
		*(result.(*string)) = hexutil.Encode(res)
	}
	return callArgs.Error(1)
}

func TestGetEnv(t *testing.T) {
	// Test case 1: Environment variable is set
	os.Setenv("TEST_ENV_VAR", "test_value")
	if getEnv("TEST_ENV_VAR", "default_value") != "test_value" {
		t.Errorf("getEnv failed for set variable")
	}
	os.Unsetenv("TEST_ENV_VAR")

	// Test case 2: Environment variable is not set, default value is used
	if getEnv("NON_EXISTENT_VAR", "default_value") != "default_value" {
		t.Errorf("getEnv failed for unset variable")
	}

	// Test case 3: Environment variable is empty string, default value is used
	os.Setenv("EMPTY_ENV_VAR", "")
	if getEnv("EMPTY_ENV_VAR", "default_value") != "default_value" {
		t.Errorf("getEnv failed for empty variable")
	}
	os.Unsetenv("EMPTY_ENV_VAR")
}

func TestCreateEnvTemplate(t *testing.T) {
	// Define the path for the temporary .env.example file
	filePath := ".env.example"

	// Clean up the file after the test
	defer os.Remove(filePath)

	// Call the function to create the template file
	err := createEnvTemplate()
	if err != nil {
		t.Fatalf("createEnvTemplate failed: %v", err)
	}

	// Check if the file was created
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("File %s was not created", filePath)
	}

	// Optionally, read the content and verify it matches the expected content
	expectedContent := `# Oracle Attestor Configuration
RPC_URL=https://testnet-rpc.diadata.org
ORACLE_ADDRESS=0x0087342f5f4c7AB23a37c045c3EF710749527c88
SIGNED_ORACLE_ADDRESS=
PRIVATE_KEY=
SYMBOL=BTC/USD
POLLING_TIME=60
DEBUG=false

# L2 Chain Configuration for Cross-Chain Intent System
L2_RPC_URL=https://testnet-rpc.diadata.org
L2_INTENT_CONTRACT=0x30c0A25a54e156487f8FF2F5836c5150A2828632
`
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", filePath, err)
	}

	if string(content) != expectedContent {
		t.Errorf("File content mismatch.\nExpected:\n%s\nGot:\n%s", expectedContent, string(content))
	}
}

func TestNewOracleClient(t *testing.T) {
	assert := assert.New(t)

	// Save original dialEthClient and restore after test
	originalDialEthClient := dialEthClient
	defer func() {
		dialEthClient = originalDialEthClient
	}()

	// Test case 1: Successful client creation
	dialEthClient = func(rawurl string) (*ethclient.Client, error) {
		return &ethclient.Client{}, nil // Return a dummy client
	}

	client, err := NewOracleClient("http://localhost:8545", "0x123", "0x456", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5ef7aed7bd14571647e0")
	assert.NoError(err)
	assert.NotNil(client)
	assert.Equal("http://localhost:8545", client.rpcURL)
	assert.Equal("0x123", client.oracleAddr)
	assert.Equal("0x456", client.signedAddr)
	assert.Equal("0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5ef7aed7bd14571647e0", client.privateKey)
	assert.Equal("0x321659b37c36c37891D055f54A1573Ef7C821156", client.fromAddress)

	// Test case 2: ethclient.Dial returns an error
	dialEthClient = func(rawurl string) (*ethclient.Client, error) {
		return nil, errors.New("connection error")
	}

	client, err = NewOracleClient("invalid_url", "0x123", "0x456", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5ef7aed7bd14571647e0")
	assert.Error(err)
	assert.Contains(err.Error(), "failed to connect to Ethereum client")
	assert.Nil(client)

	// Test case 3: Invalid private key
	dialEthClient = func(rawurl string) (*ethclient.Client, error) {
		return &ethclient.Client{}, nil
	}

	client, err = NewOracleClient("http://localhost:8545", "0x123", "0x456", "invalid_private_key")
	assert.Error(err)
	assert.Contains(err.Error(), "failed to parse private key")
	assert.Nil(client)

	// Test case 4: No private key provided (should use placeholder address)
	dialEthClient = func(rawurl string) (*ethclient.Client, error) {
		return &ethclient.Client{}, nil
	}

	client, err = NewOracleClient("http://localhost:8545", "0x123", "0x456", "")
	assert.NoError(err)
	assert.NotNil(client)
	assert.Equal("0x0000000000000000000000000000000000000000", client.fromAddress)
}

func TestGetOracleValue(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	// Create a dummy OracleClient to get the oracleABI
	dummyClient, err := NewOracleClient("http://localhost:8545", "0x0087342f5f4c7AB23a37c045c3EF710749527c88", "0x456", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5ef7aed7bd14571647e0")
	assert.NoError(err)

	// Pack the input data
	packedInput, err := dummyClient.oracleABI.Pack("getValue", "BTC/USD")
	assert.NoError(err)

	// Mock successful response
	mockRPCClient := new(MockRPCClient)
	// Mocked price and timestamp (e.g., 12345 and 1678886400)
	mockPrice := big.NewInt(12345)
	mockTimestamp := big.NewInt(1678886400)
	encodedResult, err := dummyClient.oracleABI.Pack("getValue", mockPrice, mockTimestamp)
	assert.NoError(err)

	callData := map[string]interface{}{
		"to":   common.HexToAddress("0x0087342f5f4c7AB23a37c045c3EF710749527c88").Hex(),
		"data": hexutil.Encode(packedInput),
	}
	mockRPCClient.On("CallContext", ctx, mock.AnythingOfType("*string"), "eth_call", []interface{}{callData}, "latest").Return(encodedResult, nil).Once()

	oc := &OracleClient{
		oracleAddr: "0x0087342f5f4c7AB23a37c045c3EF710749527c88",
		rpcClient:  mockRPCClient,
		oracleABI:  dummyClient.oracleABI,
	}
\t
	price, timestamp, err := oc.GetOracleValue(ctx, "BTC/USD")
	assert.NoError(err)
	assert.Equal(mockPrice, price)
	assert.Equal(mockTimestamp, timestamp)
	mockRPCClient.AssertExpectations(t)

	// Mock error during CallContext
	mockRPCClient = new(MockRPCClient)
	mockRPCClient.On("CallContext", ctx, mock.AnythingOfType("*string"), "eth_call", []interface{}{callData}, "latest").Return(nil, errors.New("RPC error")).Once()

	oc.rpcClient = mockRPCClient
	price, timestamp, err = oc.GetOracleValue(ctx, "BTC/USD")
	assert.Error(err)
	assert.Contains(err.Error(), "contract call failed")
	assert.Nil(price)
	assert.Nil(timestamp)
	mockRPCClient.AssertExpectations(t)

	// Mock invalid hex result
	mockRPCClient = new(MockRPCClient)
	mockRPCClient.On("CallContext", ctx, mock.AnythingOfType("*string"), "eth_call", []interface{}{callData}, "latest").Return([]byte("invalid hex"), nil).Once()

	oc.rpcClient = mockRPCClient
	price, timestamp, err = oc.GetOracleValue(ctx, "BTC/USD")
	assert.Error(err)
	assert.Contains(err.Error(), "failed to decode result")
	assert.Nil(price)
	assert.Nil(timestamp)
	mockRPCClient.AssertExpectations(t)

	// Mock unpack error (e.g., wrong number of outputs)
	mockRPCClient = new(MockRPCClient)
	// Return a result that won't unpack correctly (e.g., only one uint128)
	badEncodedResult, err := dummyClient.oracleABI.Pack("getValue", mockPrice)
	assert.NoError(err)
	mockRPCClient.On("CallContext", ctx, mock.AnythingOfType("*string"), "eth_call", []interface{}{callData}, "latest").Return(badEncodedResult, nil).Once()

	oc.rpcClient = mockRPCClient
	price, timestamp, err = oc.GetOracleValue(ctx, "BTC/USD")
	assert.Error(err)
	assert.Contains(err.Error(), "failed to unpack result")
	assert.Nil(price)
	assert.Nil(timestamp)
	mockRPCClient.AssertExpectations(t)
}
