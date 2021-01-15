package bchd

import (
	"fmt"
	"github.com/Ekliptor/bchd-monitor/internal/monitoring"
	"github.com/Ekliptor/bchd-monitor/pkg/notification"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"strings"
	"time"
)

type Nodes struct {
	Nodes []*Node
}

func (nodes *Nodes) GetBestBlockHeight() BestBlockHeight {
	best := BestBlockHeight{
		BlockNumber: 0,
		Received:    time.Time{},
	}
	for _, node := range nodes.Nodes {
		if node.stats.BlockHeight.BlockNumber > best.BlockNumber {
			best = node.stats.BlockHeight
		}
	}

	return best
}

type Node struct {
	// config
	Address string `mapstructure:"Address"`

	AuthenticationToken string `mapstructure:"AuthenticationToken"` // gRPC metadata with auth token
	RootCertFile        string `mapstructure:"RootCertFile"`        // path to self-signed cert
	CaDomain            string `mapstructure:"CaDomain"`            // domain to validate certificate against (to override the domain in Address)
	AllowSelfSigned     bool   `mapstructure:"AllowSelfSigned"`

	Notify []notification.NotificationReceiver `mapstructure:"Notify"`

	stats *NodeStats

	monitor *monitoring.HttpMonitoring
}

// Node stats available via HTTP API as JSON
type NodeStats struct {
	Connected       time.Time           `json:"connected"`
	BlockHeight     BestBlockHeight     `json:"block_height"`
	LostConnections []*NodeConnectError `json:"lost_connections"`
	LastNotified    time.Time           `json:"last_notified"`
}

type BestBlockHeight struct {
	BlockNumber uint32    `json:"block_number"`
	BlockTime   time.Time `json:"block_time"`
	Received    time.Time `json:"received"` // when WE saw this node got this block
}

type NodeConnectError struct {
	When  time.Time `json:"when"`
	Error error     `json:"error"`
}

// Adds a connection error such as dropped gRPC stream.
// We use this to monitor reliability of nodes.
func (n *Node) AddConnectError(err error) {
	n.stats.LostConnections = append(n.stats.LostConnections, &NodeConnectError{
		When:  time.Now(),
		Error: err,
	})
	n.updateStats()
}

func (n *Node) SetConnected() {
	n.stats.Connected = time.Now()
	n.updateStats()
}

func (n *Node) SetBlockHeight(blockHeight uint32, timestamp time.Time) {
	n.stats.BlockHeight = BestBlockHeight{
		BlockNumber: blockHeight,
		BlockTime:   timestamp,
		Received:    time.Now(),
	}
	n.updateStats()
}

// Sends the error message to the operator(s) of this node.
func (n *Node) NotifyError(msg string) error {
	// if we recently sent a notification to this node silently swallow it
	if n.stats.LastNotified.Add(n.getNotificationPause()).After(time.Now()) {
		return nil
	}

	errorList := make([]error, 0, 10)
	sendData := notification.NewNotification(fmt.Sprintf("%s node error", n.Address), msg)

	// TODO make an API call to Prometheus /metrics and include them in message?
	for _, notify := range n.Notify {
		switch notify.Method {
		case "pushover":
			push, err := notification.NewPushover(notification.PushoverConfig{
				AppToken: notify.AppToken,
				Receiver: notify.Receiver,
			})
			if err != nil {
				errorList = append(errorList, err)
				break
			}
			err = push.SendNotification(sendData)
			if err != nil {
				errorList = append(errorList, err)
				break
			}

		case "telegram":
			tele, err := notification.NewTelegram(notification.TelegramConfig{
				Token:   notify.Token,
				Channel: notify.Channel,
			})
			if err != nil {
				errorList = append(errorList, err)
				break
			}
			err = tele.SendNotification(sendData)
			if err != nil {
				errorList = append(errorList, err)
				break
			}

		case "email":
			email, err := notification.NewEmail(notification.EmailConfig{
				SmtpHost:        notify.SmtpHost,
				SmtpPort:        notify.SmtpPort,
				AllowSelfSigned: notify.AllowSelfSigned,
				FromAddress:     notify.FromAddress,
				FromPassword:    notify.FromPassword,
				RecAddress:      notify.RecAddress,
			})
			if err != nil {
				errorList = append(errorList, err)
				break
			}
			err = email.SendNotification(sendData)
			if err != nil {
				errorList = append(errorList, err)
				break
			}

		default:
			errorList = append(errorList, errors.New(fmt.Sprintf("unknown notification Method in config: %s", notify.Method)))
		}
	}

	n.stats.LastNotified = time.Now()
	if len(errorList) == 0 {
		return nil
	}
	errorMsg := ""
	for _, err := range errorList {
		errorMsg += err.Error() + "\n"
	}
	return errors.New(strings.Trim(errorMsg, "\n"))
}

// Overwrites the previous JSON event exposed to our monitoring
// HTTP API.
func (n *Node) updateStats() {
	if n.monitor != nil { // should only be nil during tests
		n.monitor.AddEvent(n.Address, n.stats)
	}
}

func (n *Node) getNotificationPause() time.Duration {
	return time.Duration(viper.GetInt("BCHD.PauseNotificationH")) * time.Hour
}
