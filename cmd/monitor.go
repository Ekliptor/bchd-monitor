package cmd

import (
	"context"
	"github.com/Ekliptor/bchd-monitor/internal/bchd"
	"github.com/Ekliptor/bchd-monitor/internal/log"
	monitoring "github.com/Ekliptor/bchd-monitor/internal/monitoring"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"sync"
)

func init() {
	rootCmd.AddCommand(WatchCmd)
}

var WatchCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Monitor BCHD instances to be running at most recent block height",
	Long: `This command will connect to BCHD instances via gRPC and ensure they are running properly.`,

	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := getLogger()
		if err != nil {
			return err
		}

		// Create the app context
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go listenExitCommand(logger, cancel)
		monitor := createMonitoringClient(ctx, logger)

		// start all main workers in separate goroutines
		var wg sync.WaitGroup

		if monitor != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				monitor.ListenHttp(ctx)
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			watchBchdNodes(ctx, logger, monitor)
		}()

		wg.Wait()
		return nil
	},
}

func watchBchdNodes(ctx context.Context, logger log.Logger, monitor *monitoring.HttpMonitoring) {
	watcher, err := bchd.NewBchdWatcher(logger, monitor)
	if err != nil {
		logger.Fatalf("Error starting BCHD monitoring: %+v", err)
	}

	watcher.ReadGrpcStreams(ctx)
}

// Create the monitoring client of our REST API to view BCHD monitoring results.
func createMonitoringClient(ctx context.Context, logger log.Logger) *monitoring.HttpMonitoring {
	if viper.GetBool("Monitoring.Enable") == false {
		return nil
	}

	// get a list of node addresses to initialize keys in our monitoring map
	nodes := &bchd.Nodes{}
	err := viper.UnmarshalKey("BCHD", nodes)
	if err != nil {
		logger.Fatalf("Error loading BCHD nodes from config for monitoring %+v", err)
	}
	nodeAddresses := make([]string, 0, len(nodes.Nodes))
	for _, node := range nodes.Nodes {
		nodeAddresses = append(nodeAddresses, node.Address)
	}

	monitor, err := monitoring.NewHttpMonitoring(monitoring.HttpMonitoringConfig{
		HttpListenAddress: viper.GetString("Monitoring.Address"),
		Events:            nodeAddresses,
	}, logger)
	if err != nil {
		logger.Fatalf("Error starting monitoring: %+v", err)
	}

	return monitor
}
