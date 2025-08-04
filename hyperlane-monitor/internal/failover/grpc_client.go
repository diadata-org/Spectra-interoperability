package failover

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/diadata.org/Spectra-interoperability/proto"
	"github.com/diadata.org/Spectra-interoperability/hyperlane-monitor/pkg/types"
)

var grpcLogger = logrus.WithField("component", "grpc-client")

// GRPCBridgeClient implements BridgeClient interface using gRPC
type GRPCBridgeClient struct {
	client pb.BridgeServiceClient
	conn   *grpc.ClientConn
}

// NewGRPCBridgeClient creates a new gRPC bridge client
func NewGRPCBridgeClient(address string) (*GRPCBridgeClient, error) {
	grpcLogger.WithField("address", address).Info("Creating gRPC bridge client")
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create gRPC connection
	conn, err := grpc.DialContext(ctx, address, 
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		grpcLogger.WithError(err).Error("Failed to connect to gRPC server")
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	client := pb.NewBridgeServiceClient(conn)
	
	grpcLogger.Info("gRPC bridge client created successfully")
	
	return &GRPCBridgeClient{
		client: client,
		conn:   conn,
	}, nil
}

// Close closes the gRPC connection
func (c *GRPCBridgeClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// CheckHealth checks if the bridge service is healthy
func (c *GRPCBridgeClient) CheckHealth(ctx context.Context) error {
	resp, err := c.client.HealthCheck(ctx, &pb.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if !resp.Healthy {
		return fmt.Errorf("bridge service is not healthy")
	}

	grpcLogger.WithFields(logrus.Fields{
		"version": resp.Version,
		"uptime":  resp.UptimeSeconds,
	}).Debug("Bridge service is healthy")

	return nil
}

// TriggerFailover sends a failover request to the bridge service
func (c *GRPCBridgeClient) TriggerFailover(ctx context.Context, req *types.FailoverRequest) (*types.FailoverResponse, error) {
	startTime := time.Now()

	// Convert types.OracleIntent to proto.OracleIntent
	var protoIntent *pb.OracleIntent
	if req.IntentData != nil {
		protoIntent = &pb.OracleIntent{
			IntentType: req.IntentData.IntentType,
			Version:    req.IntentData.Version,
			Symbol:     req.IntentData.Symbol,
			Source:     req.IntentData.Source,
			Signature:  req.IntentData.Signature,
		}
		
		// Handle potentially nil big.Int fields
		if req.IntentData.ChainID != nil {
			protoIntent.ChainId = req.IntentData.ChainID.String()
		}
		if req.IntentData.Nonce != nil {
			protoIntent.Nonce = req.IntentData.Nonce.String()
		}
		if req.IntentData.Expiry != nil {
			protoIntent.Expiry = req.IntentData.Expiry.String()
		}
		if req.IntentData.Price != nil {
			protoIntent.Price = req.IntentData.Price.String()
		}
		if req.IntentData.Timestamp != nil {
			protoIntent.Timestamp = req.IntentData.Timestamp.String()
		}
		
		// Handle signer address
		if (req.IntentData.Signer != common.Address{}) {
			protoIntent.Signer = req.IntentData.Signer.Hex()
		}
	}

	// Create gRPC request
	grpcReq := &pb.FailoverRequest{
		MessageId:          req.MessageID,
		IntentHash:         req.IntentHash,
		PairId:             req.PairID,
		SourceChainId:      int64(req.SourceChainID),
		DestinationChainId: int64(req.DestinationChainID),
		ReceiverAddress:    req.ReceiverAddress,
		IntentData:         protoIntent,
		Reason:             req.Reason,
	}

	grpcLogger.WithFields(logrus.Fields{
		"message_id":   req.MessageID,
		"intent_hash":  req.IntentHash,
		"source":       req.SourceChainID,
		"destination":  req.DestinationChainID,
		"has_intent":   protoIntent != nil,
		"receiver":     req.ReceiverAddress,
	}).Info("Sending gRPC failover request")

	// Send request
	grpcLogger.Info("About to call TriggerFailover RPC")
	resp, err := c.client.TriggerFailover(ctx, grpcReq)
	if err != nil {
		grpcLogger.WithError(err).Error("TriggerFailover RPC failed")
		return nil, fmt.Errorf("failover request failed: %w", err)
	}
	grpcLogger.Info("TriggerFailover RPC completed successfully")

	grpcLogger.WithFields(logrus.Fields{
		"request_id": resp.RequestId,
		"status":     resp.Status,
		"duration":   time.Since(startTime).Milliseconds(),
	}).Info("Failover request sent via gRPC")

	return &types.FailoverResponse{
		RequestID: resp.RequestId,
		Status:    resp.Status,
		Timestamp: time.Unix(resp.Timestamp, 0),
	}, nil
}

