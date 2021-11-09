package bchd

import (
	"context"
	"crypto/tls"
	pb "github.com/Ekliptor/bchd-monitor/internal/bchd/golang"
	"github.com/Ekliptor/bchd-monitor/internal/log"
	"github.com/Ekliptor/bchd-monitor/internal/monitoring"
	"github.com/Ekliptor/bchd-monitor/pkg/chainhash"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"io"
	"time"
)

type GRPCClient struct {
	Node   *Node
	Client pb.BchrpcClient

	logger  log.Logger
	monitor *monitoring.HttpMonitoring
	conn    *grpc.ClientConn
}

func NewGrpcClient(node Node, logger log.Logger, monitor *monitoring.HttpMonitoring) (grpcClient *GRPCClient, err error) {
	grpcClient = &GRPCClient{
		Node: &node,
		logger: logger.WithFields(log.Fields{
			"module":  "bchd_grpc",
			"address": node.Address,
		}),
		monitor: monitor,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := node.Address
	logger.Infof("Connecting to BCHD at: %s", target)
	var opts []grpc.DialOption
	if node.RootCertFile != "" {
		creds, err := credentials.NewClientTLSFromFile(node.RootCertFile, node.CaDomain)
		if err != nil {
			logger.Errorf("Failed to create gRPC TLS credentials %v", err)
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		config := &tls.Config{
			InsecureSkipVerify: node.AllowSelfSigned,
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(config)))
	}
	opts = append(opts, grpc.WithBlock())
	opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                9 * time.Minute,
		Timeout:             20 * time.Second,
		PermitWithoutStream: false,
	}))
	grpcClient.conn, err = grpc.DialContext(ctx, target, opts...)
	logger.Infof("Connecting to gRPC using TLS at %s", node.Address)
	if err != nil {
		grpcClient.logger.Errorf("%+v", err)
		return nil, err
	}

	grpcClient.Client = pb.NewBchrpcClient(grpcClient.conn)
	return grpcClient, nil
}

func (gc *GRPCClient) Close() error {
	if gc.conn == nil {
		return nil
	}
	return gc.conn.Close()
}

func NewReqContext(node Node) context.Context {
	reqCtx := context.Background()
	if node.AuthenticationToken != "" {
		reqCtx = metadata.AppendToOutgoingContext(reqCtx, "AuthenticationToken", node.AuthenticationToken)
	}
	return reqCtx
}

func (gc *GRPCClient) ReadTransactionStream(reqCtx context.Context, cancel context.CancelFunc) error {
	info, err := gc.Client.GetBlockchainInfo(context.Background(), &pb.GetBlockchainInfoRequest{})
	if err != nil {
		gc.logger.Errorf("Getting chain info err %+v", err)
		return err
	}
	hash, err := chainhash.NewHash(info.GetBestBlockHash())
	if err != nil {
		return errors.Wrap(err, "invalid chain hash from GetBlockchainInfo")
	}
	gc.logger.Infof("Chain info from %s: height %d, hash %s", gc.Node.Address,
		info.GetBestHeight(), hash.String())
	gc.Node.SetConnected()
	gc.Node.SetBlockHeight(uint32(info.GetBestHeight()), time.Unix(0, 0))

	// open the TX stream
	transactionStream, err := gc.Client.SubscribeTransactions(reqCtx, &pb.SubscribeTransactionsRequest{
		Subscribe: &pb.TransactionFilter{
			AllTransactions: true,
		},
		Unsubscribe:    nil,
		IncludeMempool: false,
		IncludeInBlock: true,
		SerializeTx:    false,
	})
	if err != nil {
		gc.logger.Errorf("Error subscribing to BCHD TX stream: %+v", err)
		cancel()
		return err
	}
	gc.logger.Infof("Opened TX stream from BCHD at %s", gc.Node.Address)
	for {
		data, err := transactionStream.Recv()
		if err == io.EOF {
			gc.logger.Errorf("BCHD TX stream stopped. This shouldn't happen!")
			cancel()
			gc.Node.AddConnectError(errors.Wrap(err, "EOF error in TX stream"))
			break
		} else if err != nil {
			gc.logger.Errorf("Error in BCHD TX stream: %+v", err)
			cancel()
			gc.Node.AddConnectError(errors.Wrap(err, "TX stream closed"))
			break
		}

		//gc.logger.Debugf("TX: %+v", data)
		gc.Node.SetLastReceive()
		if data.GetType() == pb.TransactionNotification_CONFIRMED {
			tx := data.GetConfirmedTransaction()
			if tx == nil {
				gc.logger.Errorf("Received invalid BCHD TX")
				continue
			}

			curHeight := uint32(tx.GetBlockHeight())
			if curHeight != gc.Node.stats.BlockHeight.BlockNumber {
				gc.logger.Debugf("Node %s received new block %d -> %d", gc.Node.Address, gc.Node.stats.BlockHeight.BlockNumber, curHeight)
				gc.Node.SetBlockHeight(curHeight, time.Unix(tx.GetTimestamp(), 0))
			}
		}
	}

	return nil
}
