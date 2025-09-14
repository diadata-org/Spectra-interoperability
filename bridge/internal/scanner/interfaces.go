package scanner

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/diadata.org/Spectra-interoperability/bridge/internal/database"
	bridgeTypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

// EthereumClient interface defines the methods needed from ethclient.Client
type EthereumClient interface {
	BlockNumber(ctx context.Context) (uint64, error)
	FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)
	Close()
}

// DatabaseInterface defines the methods needed from database.DB
type DatabaseInterface interface {
	InitializeChainState(chainID int64, name string, startBlock uint64) error
	GetChainState(chainID int64) (*database.ChainState, error)
	UpdateLastScanBlock(chainID int64, blockNumber uint64) error
	IsEventProcessed(intentHash string) (bool, error)
	MarkEventProcessed(event *bridgeTypes.EventData)
	GetProcessedEventsByBlockRange(startBlock, endBlock uint64) ([]*database.ProcessedEvent, error)
}

// databaseAdapter wraps database.DB to implement DatabaseInterface
type databaseAdapter struct {
	db *database.DB
}

// NewDatabaseAdapter creates a new databaseAdapter
func NewDatabaseAdapter(db *database.DB) DatabaseInterface {
	return &databaseAdapter{db: db}
}

// Implement DatabaseInterface methods for databaseAdapter
func (da *databaseAdapter) InitializeChainState(chainID int64, name string, startBlock uint64) error {
	return da.db.InitializeChainState(chainID, name, startBlock)
}

func (da *databaseAdapter) GetChainState(chainID int64) (*database.ChainState, error) {
	return da.db.GetChainState(chainID)
}

func (da *databaseAdapter) UpdateLastScanBlock(chainID int64, blockNumber uint64) error {
	return da.db.UpdateLastScanBlock(chainID, blockNumber)
}

func (da *databaseAdapter) IsEventProcessed(intentHash string) (bool, error) {
	return da.db.IsEventProcessed(intentHash)
}

func (da *databaseAdapter) MarkEventProcessed(event *bridgeTypes.EventData) {
	processedEvent := &database.ProcessedEvent{
		EventName:       event.EventName,
		IntentHash:      common.BytesToHash(event.IntentHash[:]).Hex(),
		BlockNumber:     event.BlockNumber,
		TransactionHash: func() string {
				if event.TxHash == (common.Hash{}) {
					return ""
				}
				return event.TxHash.Hex()
			}(),
		LogIndex:        event.LogIndex,
		Symbol:          event.Symbol,
		Price:           func() string {
				if event.Price != nil {
					return event.Price.String()
				}
				return "0" // Default price
			}(),
		Timestamp:       func() uint64 {
				if event.Timestamp != nil {
					return event.Timestamp.Uint64()
				}
				return 0 // Default timestamp
			}(),
		Signer:          event.Signer,
		ProcessedAt:     time.Now(),
	}

	// event.EventID is not part of bridgeTypes.EventData, so no handling needed here.

	da.db.SaveProcessedEvent(processedEvent)
}

func (da *databaseAdapter) GetProcessedEventsByBlockRange(startBlock, endBlock uint64) ([]*database.ProcessedEvent, error) {
	return da.db.GetProcessedEventsByBlockRange(startBlock, endBlock)
}
