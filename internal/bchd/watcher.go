package bchd

import (
	"context"
	"fmt"
	"github.com/Ekliptor/bchd-monitor/internal/log"
	"github.com/Ekliptor/bchd-monitor/internal/monitoring"
	"github.com/Ekliptor/bchd-monitor/pkg/trace"
	"github.com/Ekliptor/bchd-monitor/utils"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"net"
	"time"
)

type BchdWatcher struct {
	Nodes                        *Nodes
	BlocksBehindWarning          uint32
	LatestBlockWithin            time.Duration
	MaxDroppedConnectionsPerHour uint32

	logger  log.Logger
	monitor *monitoring.HttpMonitoring
}

func NewBchdWatcher(logger log.Logger, monitor *monitoring.HttpMonitoring) (*BchdWatcher, error) {
	watcher := &BchdWatcher{
		Nodes: nil,
		logger: logger.WithFields(log.Fields{
			"module": "bchd_watcher",
		}),
		monitor: monitor,
	}
	err := watcher.loadNodeConfig()
	if err != nil {
		return nil, err
	}
	return watcher, nil
}

func (w *BchdWatcher) ReadGrpcStreams(ctx context.Context) {
	for _, node := range w.Nodes.Nodes {
		go w.readGrpcStream(node, ctx)
	}

	// evaluate stats & cleanup old data
	tickerInterval := time.Duration(1) * time.Minute
	var ticker = time.NewTicker(tickerInterval)
	terminating := false
	for !terminating {
		select {
		case <-ticker.C:
			//ticker = time.NewTicker(tickerInterval) // start a new ticker
			w.evalNodeStats()
			w.monitorNodeConnections()

		case <-ctx.Done():
			terminating = true
			break
		}
	}
}

func (w *BchdWatcher) readGrpcStream(node *Node, ctx context.Context) {
	var err error
	var client *GRPCClient
	node.requestContext, node.cancelReqCtx = context.WithTimeout(context.Background(), 1*time.Millisecond) // dummy context ending immediately to start the loop

	terminating := false
	for !terminating {
		select {
		case <-node.requestContext.Done():
			if client != nil {
				// something went wrong with our BCHD TX stream, retry
				w.logger.Errorf("Error in gRPC connection, retrying...")
				client.Close()
				time.Sleep(10 * time.Second)
			}

			client, err = NewGrpcClient(*node, w.logger, w.monitor)
			if err != nil {
				w.logger.Errorf("Error creating bchd gRPC client: %+v", err)
				break
			}
			//defer client.Close()
			node.requestContext, node.cancelReqCtx = context.WithCancel(NewReqContext(*node))
			go client.ReadTransactionStream(node.requestContext, node.cancelReqCtx)

		case <-ctx.Done():
			client.Close()
			terminating = true
		}
	}
}

func (w *BchdWatcher) evalNodeStats() {
	now := time.Now()
	bestBlockHeight := w.Nodes.GetBestBlockHeight()

	for _, node := range w.Nodes.Nodes {
		if len(node.stats.LostConnections) != 0 {
			// remove lost connections older than 1 hour
			lostConnections := make([]*NodeConnectError, 0, len(node.stats.LostConnections))
			for _, err := range node.stats.LostConnections {
				if err.When.Add(1 * time.Hour).After(now) {
					lostConnections = append(lostConnections, err) // this error happened recently
				}
			}
			node.stats.LostConnections = lostConnections
		}

		// send notification if node has too many lost connections
		if w.MaxDroppedConnectionsPerHour != 0 && len(node.stats.LostConnections) > int(w.MaxDroppedConnectionsPerHour) {
			msg := fmt.Sprintf("%d dropped connection within the last 60 min.", len(node.stats.LostConnections))
			traceroute := w.getNodeTraceroute(node)
			if len(traceroute) != 0 {
				msg += "\r\n\r\n" + traceroute
			}
			w.logger.Errorf("Node Error: %s", msg)
			err := node.NotifyError(msg)
			if err != nil {
				w.logger.Errorf("Error sending 'connection lost' message %+v", err)
			}
		}

		// check if best block ist behind other nodes
		if w.BlocksBehindWarning > 0 && node.stats.BlockHeight.BlockNumber+w.BlocksBehindWarning <= bestBlockHeight.BlockNumber &&
			!utils.IsZero(bestBlockHeight.BlockTime) && !utils.IsZero(node.stats.BlockHeight.BlockTime) {
			msg := fmt.Sprintf("Node is behind:\r\nbest block (network) %d at %s\r\nbest block (node) %d at %s",
				bestBlockHeight.BlockNumber, bestBlockHeight.BlockTime.Format(time.RFC3339),
				node.stats.BlockHeight.BlockNumber, node.stats.BlockHeight.BlockTime.Format(time.RFC3339))
			w.logger.Errorf("Node Error: %s", msg)
			err := node.NotifyError(msg)
			if err != nil {
				w.logger.Errorf("Error sending 'blocks behind' message %+v", err)
			}
		}

		// check if it takes too long to receive the best block
		if w.LatestBlockWithin.Seconds() > 0 {
			diffSec := node.stats.BlockHeight.Received.Unix() - bestBlockHeight.Received.Unix()
			if diffSec > int64(w.LatestBlockWithin.Seconds()) {
				msg := fmt.Sprintf("Node is receiving blocks late:\r\nbest block (network) %s\r\nbest block (node) %s",
					bestBlockHeight.Received.Format(time.RFC3339), node.stats.BlockHeight.Received.Format(time.RFC3339))
				w.logger.Errorf("Node Error: %s", msg)
				err := node.NotifyError(msg)
				if err != nil {
					w.logger.Errorf("Error sending 'blocks late' message %+v", err)
				}
			}
		}
	}
}

func (w *BchdWatcher) monitorNodeConnections() {
	maxAgeAlive := time.Now().Add(-1 * time.Minute * time.Duration(viper.GetInt("BCHD.MaxAgeLastReceiveMin")))
	for _, node := range w.Nodes.Nodes {
		if node.stats.LastReceive.Before(maxAgeAlive) {
			if node.cancelReqCtx == nil {
				w.logger.Errorf("Node %s has no cancel stream function attached", node.Address)
				continue
			}
			w.logger.Warnf("Received no data from node %s since %s - forcing reconnect", node.Address, time.Since(node.stats.LastReceive))
			node.cancelReqCtx()
		}
	}
}

func (w *BchdWatcher) getNodeTraceroute(node *Node) string {
	host, _, err := net.SplitHostPort(node.Address)
	if err != nil {
		w.logger.Errorf("Error to split host:port of node address %s: %+v", node.Address, err)
		return ""
	}
	tracer, err := trace.NewTraceFromHost(host, nil)
	if err != nil {
		w.logger.Errorf("Error setting up traceroute: %+v", err)
		return ""
	}
	err = tracer.Run()
	if err != nil {
		w.logger.Errorf("Error getting traceroute: %+v", err)
		return ""
	}

	w.logger.Debugf("Node trace:\n%s", tracer.String())
	return tracer.String()
}

func (w *BchdWatcher) loadNodeConfig() error {
	w.Nodes = &Nodes{}
	err := viper.UnmarshalKey("BCHD", w.Nodes)
	if err != nil {
		return errors.Wrap(err, "error loading BCHD nodes from config")
	}

	for _, node := range w.Nodes.Nodes {
		node.monitor = w.monitor
		node.stats = &NodeStats{}
	}

	w.BlocksBehindWarning = viper.GetUint32("BCHD.BlocksBehindWarning")
	w.LatestBlockWithin = time.Duration(viper.GetInt("BCHD.LatestBlockWithinSec")) * time.Second
	w.MaxDroppedConnectionsPerHour = viper.GetUint32("BCHD.MaxDroppedConnectionsPerHour")

	return nil
}
