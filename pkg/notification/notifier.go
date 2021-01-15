package notification

import (
	"github.com/spf13/viper"
	"net/http"
	"time"
)

type Notifier interface {
	SendNotification(notification *Notification) error
}

type NotificationMethod string

const (
	NOTIFICATION_EMAIL    = "email"
	NOTIFICATION_PUSHOVER = "pushover"
	NOTIFICATION_TELEGRAM = "telegram"
)

type NotificationReceiver struct {
	Method NotificationMethod `mapstructure:"Method"`

	// Email
	SmtpHost        string `mapstructure:"SmtpHost"`
	SmtpPort        int    `mapstructure:"SmtpPort"`
	AllowSelfSigned bool   `mapstructure:"AllowSelfSigned"`
	FromAddress     string `mapstructure:"FromAddress"`
	FromPassword    string `mapstructure:"FromPassword"`
	RecAddress      string `mapstructure:"RecAddress"`

	// Pushover
	AppToken string `mapstructure:"AppToken"`
	Receiver string `mapstructure:"Receiver"`

	// Telegram
	Token   string `mapstructure:"Token"`
	Channel string `mapstructure:"Channel"`
}

func getHttpAgent() *http.Client {
	return &http.Client{
		Timeout: time.Duration(viper.GetInt("HTTP.RequestTimeoutSec")) * time.Second,
	}
}
