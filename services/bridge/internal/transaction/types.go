package transaction

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"

	bridgetypes "github.com/diadata.org/Spectra-interoperability/services/bridge/internal/types"
)

type Request struct {
	Ctx           context.Context
	ContractAddr  string
	MethodName    string
	ABI           string
	Params        []interface{}
	GasPrice      *big.Int
	GasLimit      uint64
	UpdateRequest *bridgetypes.UpdateRequest
}

type Result struct {
	Tx  *types.Transaction
	Err error
}

type ExecutorFunc func(ctx context.Context) (*types.Transaction, error)
