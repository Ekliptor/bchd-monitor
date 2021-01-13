package bchd

import (
	"context"
	"github.com/Ekliptor/bchd-monitor/internal/log"
	"github.com/spf13/viper"
	"testing"
	"time"
)

const testNodeAddress = "bchd.cashtippr.com:8335"

func TestGrpcStream(t *testing.T) {
	logger, _ := getLogger()
	node := Node{
		Address:             testNodeAddress,
		AllowSelfSigned:     true,
		monitor:             nil,
	}
	client, err := NewGrpcClient(node, logger, nil)
	if err != nil {
		t.Fatalf("Error creating gRPC client %+v", err)
	}

	// read for 5 seconds
	reqContext, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	err = client.ReadTransactionStream(reqContext, cancel)
	if err != nil {
		t.Fatalf("Error reating gRPC TX stream %+v", err)
	}
	// TODO test more ?

	t.Log("Successfully read data from gRPC stream")
}

func getLogger() (log.Logger, error) {
	return log.NewLogger(log.NewConfig(viper.GetViper()), log.DefaultLogger)
}
