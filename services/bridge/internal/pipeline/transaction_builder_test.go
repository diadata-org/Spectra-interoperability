package pipeline

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"
)

type TransactionBuilderTestSuite struct {
	suite.Suite
}

func TestTransactionBuilderTestSuite(t *testing.T) {
	suite.Run(t, new(TransactionBuilderTestSuite))
}

func (s *TransactionBuilderTestSuite) TestExtractRevertReasonParsed() {
	err := errors.New("rpc error: code = 3 desc = execution reverted: insufficient funds")
	reason := extractRevertReason(err)

	s.Equal("insufficient funds", reason)
}

func (s *TransactionBuilderTestSuite) TestExtractRevertReasonEmpty() {
	err := errors.New("random error")
	s.Empty(extractRevertReason(err))
}
