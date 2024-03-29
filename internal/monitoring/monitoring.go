package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ekliptor/bchd-monitor/internal/log"
	"net/http"
	"sync"
	"time"
)

// ensure we always implement http.Handler (compile error otherwise)
var _ http.Handler = (*HttpMonitoring)(nil)

// D is a shortcut to use for adding events
type D map[string]interface{}

// EventMap contains the map of all registered events
type EventMap map[string]*Event

// Event is a single event.
type Event struct {
	Data interface{} `json:"data"`
	When int64       `json:"when"` // unix timestamp
}

type HttpMonitoringConfig struct {
	HttpListenAddress string   // for example ":8080"
	Path              string   // the HTTP path to serve monitoring data on - defaults to "/monitoring"
	Events            []string // A list of event names to keep track of
}

// The monitoring server with API to add events.
type HttpMonitoring struct {
	config HttpMonitoringConfig
	logger log.Logger
	mu     sync.RWMutex

	events EventMap
}

func NewHttpMonitoring(config HttpMonitoringConfig, logger log.Logger) (*HttpMonitoring, error) {
	if len(config.Path) == 0 {
		config.Path = "/monitoring"
	}
	m := &HttpMonitoring{
		config: config,
		logger: logger.WithFields(
			log.Fields{
				"module": "monitoring",
			},
		),
		events: make(EventMap, len(config.Events)),
	}

	// copy all event keys
	for _, key := range config.Events {
		m.events[key] = nil
	}

	return m, nil
}

// Starts to serve monitoring data on the configured address.
// This call is blocking.
func (m *HttpMonitoring) ListenHttp(ctx context.Context) error {
	s := &http.Server{
		Addr:        m.config.HttpListenAddress,
		Handler:     m,
		ReadTimeout: 30 * time.Second,
		//ErrorLog:    goLog.New(log.GetWriter(), "HTTP: ", 0),
	}

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		if err := s.Shutdown(context.Background()); err != nil {
			m.logger.Errorf("Error on HTTP server shutdown: %+v", err)
		}
		close(done)
	}()

	m.logger.Infof("Serving HTTP monitoring JSON on %s%s", m.config.HttpListenAddress, m.config.Path)
	if err := s.ListenAndServe(); err != nil {
		return err
	}

	<-done // wait for server shutdown once main context ends
	return nil
}

// AddEvent adds an event with the current timestamp. It overwrites the previous event
// of the same name (if existing).
func (m *HttpMonitoring) AddEvent(name string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.events[name]
	if exists == false { // only allow pre-registered events to prevent consuming too much memory
		return errors.New(fmt.Sprintf("can not add unknown event '%s' - please add it to config first", name))
	}
	m.events[name] = &Event{
		Data: value,
		When: time.Now().Unix(),
	}
	return nil
}

// GetEvent returns a registered event value.
func (m *HttpMonitoring) GetEvent(name string) *Event {
	m.mu.RLock()
	defer m.mu.RUnlock()

	event, exists := m.events[name]
	if exists == false {
		m.logger.Errorf("Can not fetch unregistered event: %s", name)
		return nil
	}
	return event
}

type MonitoringHttpResponse struct {
	Error bool     `json:"error"`
	Data  EventMap `json:"data"`
	Time  int64    `json:"time"` // unix timestamp
}

// Respond with monitoring data to an HTTP request.
// Only needed if you want to integrate the monitoring package into another
// HTTP server (instead of calling HttpMonitoring.ListenHttp() ).
func (m *HttpMonitoring) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" || req.URL.Path != m.config.Path {
		writer.WriteHeader(http.StatusNotFound)
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Write([]byte("Not found"))
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	res := MonitoringHttpResponse{
		Error: false,
		Data:  m.events,
		Time:  time.Now().Unix(),
	}
	jsonData, err := json.Marshal(res)
	if err != nil {
		m.logger.Errorf("Error responding monitoring data: %+v", err)

		res.Error = true
		res.Data = nil // the payload most likely caused the error
		jsonData, err := json.Marshal(res)
		if err != nil {
			m.logger.Errorf("Repeating responding monitoring data - giving up: %+v", err)
			return
		}
		writer.WriteHeader(http.StatusInternalServerError)
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Write(jsonData)
		return
	}

	writer.WriteHeader(http.StatusOK)
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Write(jsonData)
}
