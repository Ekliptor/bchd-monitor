package trace

import (
	"fmt"
	"github.com/aeden/traceroute"
	"github.com/pkg/errors"
	"net"
	"strings"
	"syscall"
)

// Provides a traceroute to a given host.
type Trace struct {
	// Results
	Result *traceroute.TracerouteResult
	IpAddr *net.IPAddr

	// Config
	Host    string // hostname or IP address
	options *traceroute.TracerouteOptions
}

func NewTrace(ipAddr *net.IPAddr, options *traceroute.TracerouteOptions) *Trace {
	if options == nil {
		options = &traceroute.TracerouteOptions{}
		options.SetRetries(0)
		options.SetMaxHops(traceroute.DEFAULT_MAX_HOPS + 1)
		options.SetFirstHop(traceroute.DEFAULT_FIRST_HOP)
	}

	return &Trace{
		Result:  nil,
		IpAddr:  ipAddr,
		Host:    ipAddr.String(),
		options: options,
	}
}

func NewTraceFromHost(host string, options *traceroute.TracerouteOptions) (*Trace, error) {
	ipAddr, err := net.ResolveIPAddr("ip", host)
	if err != nil {
		return nil, err
	}
	trace := NewTrace(ipAddr, options)
	trace.Host = host
	return trace, nil
}

func (t *Trace) Run() error {
	result, err := traceroute.Traceroute(t.Host, t.options)
	if err != nil {
		if err == syscall.EPERM {
			return errors.Wrap(err, "unable to open raw socket for ICMP. On some operating systems (OSX) this requires root privileges.")
		}
		return err
	}
	t.Result = &result
	return nil
}

// Returns a traceroute as string with 1 hop per line.
func (t *Trace) String() string {
	if t.Result == nil {
		return "no result"
	}

	hopMsg := ""
	for _, hop := range t.Result.Hops {
		addr := fmt.Sprintf("%v.%v.%v.%v", hop.Address[0], hop.Address[1], hop.Address[2], hop.Address[3])
		hostOrAddr := addr
		if hop.Host != "" {
			hostOrAddr = hop.Host
		}
		if hop.Success {
			hopMsg += fmt.Sprintf("%-3d %v (%v)  %v\n", hop.TTL, hostOrAddr, addr, hop.ElapsedTime)
		} else {
			hopMsg += fmt.Sprintf("%-3d *\n", hop.TTL)
		}
	}

	return strings.Trim(hopMsg, "\n")
}
