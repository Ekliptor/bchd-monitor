package cmd

import (
	"context"
	"fmt"
	"github.com/Ekliptor/bchd-monitor/pkg/trace"
	"github.com/aeden/traceroute"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type TraceParams struct {
	MaxHops  int
	FirstHop int
	Probes   int
}

var traceParams = &TraceParams{}

func init() {
	TraceCmd.Flags().IntVar(&traceParams.MaxHops, "m", traceroute.DEFAULT_MAX_HOPS, "Set the max time-to-live (max number of hops) used in outgoing probe packets (default is 64)")
	TraceCmd.Flags().IntVar(&traceParams.FirstHop, "f", traceroute.DEFAULT_MAX_HOPS, "Set the first used time-to-live, e.g. the first hop (default is 1)")
	TraceCmd.Flags().IntVar(&traceParams.Probes, "q", traceroute.DEFAULT_MAX_HOPS, "Set the number of probes per \"ttl\" to nqueries (default is one probe).")

	rootCmd.AddCommand(TraceCmd)
}

var TraceCmd = &cobra.Command{
	Use:   "trace",
	Short: "Perform a traceroute to a given host",

	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := getLogger()
		if err != nil {
			return err
		}

		// Create the app context
		_, cancel := context.WithCancel(context.Background())
		defer cancel()
		go listenExitCommand(logger, cancel)

		// read args
		if len(args) < 1 {
			return errors.New(fmt.Sprintf("%s command requires 1 argument: host", cmd.Name()))
		}
		logger.Infof("Starting traceroute for host %s ...", args[0])
		options := &traceroute.TracerouteOptions{}
		options.SetRetries(traceParams.Probes - 1)
		options.SetMaxHops(traceParams.MaxHops + 1)
		options.SetFirstHop(traceParams.FirstHop)

		// run the traceroute
		tracer, err := trace.NewTraceFromHost(args[0], options)
		if err != nil {
			return errors.Wrap(err, "error setting up traceroute")
		}
		err = tracer.Run()
		if err != nil {
			return errors.Wrap(err, "error getting traceroute")
		}

		logger.Infof("Node trace:\n%s", tracer.String())

		return nil
	},
}
