package grpc

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/diadata.org/Spectra-interoperability/proto"
	"github.com/diadata.org/Spectra-interoperability/bridge/internal/api"
	bridgetypes "github.com/diadata.org/Spectra-interoperability/bridge/internal/types"
)

var logger = logrus.WithField("component", "grpc-server")

// Server implements the gRPC BridgeService
type Server struct {
	pb.UnimplementedBridgeServiceServer
	failoverHandler *api.FailoverHandler
	startTime       time.Time
}

// NewServer creates a new gRPC server
func NewServer(failoverHandler *api.FailoverHandler) *Server {
	return &Server{
		failoverHandler: failoverHandler,
		startTime:       time.Now(),
	}
}

// Start starts the gRPC server on the specified port
func (s *Server) Start(port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(loggingInterceptor),
	)
	pb.RegisterBridgeServiceServer(grpcServer, s)

	logger.WithField("port", port).Info("Starting gRPC server")
	logger.WithField("address", lis.Addr().String()).Info("gRPC server listening on address")
	return grpcServer.Serve(lis)
}

// loggingInterceptor logs all incoming gRPC requests
func loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	logger.WithFields(logrus.Fields{
		"method": info.FullMethod,
		"start": start.Format(time.RFC3339),
	}).Info("gRPC request received")
	
	resp, err := handler(ctx, req)
	
	logger.WithFields(logrus.Fields{
		"method": info.FullMethod,
		"duration": time.Since(start).String(),
		"error": err,
	}).Info("gRPC request completed")
	
	return resp, err
}

// TriggerFailover handles failover requests via gRPC
func (s *Server) TriggerFailover(ctx context.Context, req *pb.FailoverRequest) (*pb.FailoverResponse, error) {
	logger.WithFields(logrus.Fields{
		"message_id":   req.MessageId,
		"intent_hash":  req.IntentHash,
		"source":       req.SourceChainId,
		"destination":  req.DestinationChainId,
		"receiver":     req.ReceiverAddress,
	}).Info("Received gRPC failover request")

	// Validate request
	if req.MessageId == "" {
		return nil, status.Error(codes.InvalidArgument, "message_id is required")
	}
	if req.IntentData == nil {
		return nil, status.Error(codes.InvalidArgument, "intent_data is required")
	}

	// Convert proto intent to internal type
	intent, err := protoToIntent(req.IntentData)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid intent data: %v", err)
	}

	// Create internal failover request
	internalReq := api.FailoverRequest{
		MessageID:          req.MessageId,
		IntentHash:         req.IntentHash,
		PairID:             req.PairId,
		SourceChainID:      int(req.SourceChainId),
		DestinationChainID: int(req.DestinationChainId),
		ReceiverAddress:    req.ReceiverAddress,
		IntentData:         intent,
		Reason:             req.Reason,
	}

	// Generate request ID
	requestID := uuid.New().String()

	// Process failover (this will be done asynchronously)
	go s.failoverHandler.ProcessFailoverRequest(requestID, internalReq)

	return &pb.FailoverResponse{
		RequestId: requestID,
		Status:    "accepted",
		Timestamp: time.Now().Unix(),
		Message:   "Failover request accepted for processing",
	}, nil
}

// GetFailoverStatus returns the status of a failover request
func (s *Server) GetFailoverStatus(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	if req.RequestId == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id is required")
	}

	failoverStatus := s.failoverHandler.GetStatus(req.RequestId)
	if failoverStatus == nil {
		return nil, status.Error(codes.NotFound, "request not found")
	}

	return &pb.StatusResponse{
		RequestId:    failoverStatus.RequestID,
		Status:       failoverStatus.Status,
		TxHash:       failoverStatus.TransactionHash,
		ErrorMessage: failoverStatus.Error,
		CreatedAt:    failoverStatus.CreatedAt.Unix(),
		UpdatedAt:    failoverStatus.UpdatedAt.Unix(),
	}, nil
}

// HealthCheck returns the health status of the bridge
func (s *Server) HealthCheck(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	// Get chain status from failover handler
	chainStatus := make(map[string]*pb.ChainStatus)
	
	// Add chain status information (this would come from the failover handler)
	// For now, we'll return a simple healthy status
	
	return &pb.HealthResponse{
		Healthy:       true,
		Version:       "1.0.0",
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		ChainStatus:   chainStatus,
	}, nil
}

// protoToIntent converts a proto OracleIntent to internal type
func protoToIntent(proto *pb.OracleIntent) (*bridgetypes.OracleIntent, error) {
	// Initialize with zero values
	intent := &bridgetypes.OracleIntent{
		IntentType: proto.IntentType,
		Version:    proto.Version,
		Symbol:     proto.Symbol,
		Source:     proto.Source,
		Signature:  proto.Signature,
		ChainID:    new(big.Int),
		Nonce:      new(big.Int),
		Expiry:     new(big.Int),
		Price:      new(big.Int),
		Timestamp:  new(big.Int),
	}

	// Parse ChainID
	if proto.ChainId != "" {
		chainId, ok := new(big.Int).SetString(proto.ChainId, 10)
		if !ok {
			return nil, fmt.Errorf("invalid chain_id: %s", proto.ChainId)
		}
		intent.ChainID = chainId
	}

	// Parse Nonce
	if proto.Nonce != "" {
		nonce, ok := new(big.Int).SetString(proto.Nonce, 10)
		if !ok {
			return nil, fmt.Errorf("invalid nonce: %s", proto.Nonce)
		}
		intent.Nonce = nonce
	}

	// Parse Expiry
	if proto.Expiry != "" {
		expiry, ok := new(big.Int).SetString(proto.Expiry, 10)
		if !ok {
			return nil, fmt.Errorf("invalid expiry: %s", proto.Expiry)
		}
		intent.Expiry = expiry
	}

	// Parse Price
	if proto.Price != "" {
		price, ok := new(big.Int).SetString(proto.Price, 10)
		if !ok {
			return nil, fmt.Errorf("invalid price: %s", proto.Price)
		}
		intent.Price = price
	}

	// Parse Timestamp
	if proto.Timestamp != "" {
		timestamp, ok := new(big.Int).SetString(proto.Timestamp, 10)
		if !ok {
			return nil, fmt.Errorf("invalid timestamp: %s", proto.Timestamp)
		}
		intent.Timestamp = timestamp
	}

	// Parse Signer
	if proto.Signer != "" && common.IsHexAddress(proto.Signer) {
		intent.Signer = common.HexToAddress(proto.Signer)
	}

	return intent, nil
}