package ovpnutil

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotConnected    = errors.New("not connected")
	ErrNoMatch         = errors.New("no match")
	ErrReadTimeout     = errors.New("read timeout")
	ErrClosed          = errors.New("closed")
	ErrNotEnoughFields = errors.New("not enough fields")
	ErrInvalidData     = errors.New("invalid data")

	rePid = regexp.MustCompile(`.*pid=(\d+)`)
)

// A Signal is a number describing a process signal. It implements os.Signal interface.
type Signal int

func (s Signal) Signal() {
}

func (s Signal) String() string {
	switch s {
	case SIGHUP:
		return "SIGHUP"
	case SIGUSER1:
		return "SIGUSER1"
	case SIGUSER2:
		return "SIGUSER2"
	case SIGTERM:
		return "SIGTERM"
	}
	return ""
}

// List of signals can be used in the openvpn management interface
const (
	SIGHUP   = Signal(0x1)
	SIGUSER1 = Signal(0xa)
	SIGUSER2 = Signal(0xc)
	SIGTERM  = Signal(0xf)
)

// State represent an openvpn state
type State struct {
	Time        time.Time
	Name        string
	Description string
	LocalIP     string
	RemoteIP    string
}

func ParseState(line string) (State, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 5 {
		return State{}, ErrNotEnoughFields
	}
	t, err := strconv.ParseInt(fields[0], 0, 32)
	if err != nil {
		return State{}, err
	}
	return State{
		Time:        time.Unix(t, 0),
		Name:        strings.Trim(fields[1], " "),
		Description: strings.Trim(fields[2], " "),
		LocalIP:     strings.Trim(fields[3], " "),
		RemoteIP:    strings.Trim(fields[4], " "),
	}, nil
}

// Status openvpn
type Status struct {
	Clients     []Client
	Routing     []Route
	GlobalStats GlobalStats
}

// GlobalStats
type GlobalStats struct {
	Queue string
}

// Client represent an openvpn status user
type Client struct {
	VirtualIP      string
	RealIP         string
	CommonName     string
	BytesReceived  string
	BytesSent      string
	ConnectedSince string
}

func ParseClient(line string) (Client, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 5 {
		return Client{}, ErrNotEnoughFields
	}

	return Client{
		VirtualIP:      strings.Trim(fields[0], " "),
		RealIP:         strings.Trim(fields[1], " "),
		CommonName:     strings.Trim(fields[0], " "),
		BytesReceived:  strings.Trim(fields[2], " "),
		BytesSent:      strings.Trim(fields[3], " "),
		ConnectedSince: strings.Trim(fields[4], " "),
	}, nil
}

// Routing table
type Route struct {
	VirtualIP  string
	RealIP     string
	CommonName string
	LastRef    string
}

func (r *Route) VirtualIsIPv4() bool {
	return isIPv4(r.VirtualIP)
}

func (r *Route) VirtualIsIPv6() bool {
	return isIPv6(r.VirtualIP)
}

func (r *Route) RealIsIPv4() bool {
	return isIPv4(r.VirtualIP)
}

func (r *Route) RealIsIPv6() bool {
	return isIPv6(r.VirtualIP)
}

func ParseRoute(line string) (Route, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 4 {
		return Route{}, ErrNotEnoughFields
	}

	return Route{
		VirtualIP:  strings.Trim(fields[0], " "),
		RealIP:     strings.Trim(fields[2], " "),
		CommonName: strings.Trim(fields[1], " "),
		LastRef:    strings.Trim(fields[3], " "),
	}, nil
}

// Controller controls openvpn process via its management interface
type Controller interface {
	Signal(sig os.Signal) error
	Close()
	Getpid() (int, error)
	GetLogs() (string, error)
	GetStates() ([]State, error)
	GetStatus() (Status, error)
	GetClients() ([]Client, error)
	GetRouting() ([]Route, error)
	GetGlobalStats() (GlobalStats, error)
	SubscribeState(ch chan State) error
	SubscribeByteCount(chIn, chOut chan int64) error
	SubscribeLog(ch chan string) error
}

// Dial connects to an openvpn management interface and returns a Controller
func Dial(addr string) (Controller, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return newDefaultController(conn), nil
}

type defaultController struct {
	closed             chan bool
	output             chan string
	conn               net.Conn
	mutex              sync.Mutex
	statsSubscribers   []chan State
	byteInSubscribers  []chan int64
	byteOutSubscribers []chan int64
	logSubscribers     []chan string
}

func newDefaultController(conn net.Conn) *defaultController {
	controller := &defaultController{
		conn:   conn,
		closed: make(chan bool),
		output: make(chan string, 100),
	}
	go controller.listen()
	return controller
}

func (c *defaultController) Signal(sig os.Signal) error {
	if c.conn == nil {
		return ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	cmd := fmt.Sprintf("signal %s\n", sig.String())
	if _, err := c.conn.Write([]byte(cmd)); err != nil {
		return err
	}
	return nil
}

func (c *defaultController) Close() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil {
		c.conn.Write([]byte("exit\n"))
	}
}

func (c *defaultController) Getpid() (int, error) {
	if c.conn == nil {
		return 0, ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	if _, err := c.conn.Write([]byte("pid\n")); err != nil {
		return 0, err
	}

	// read output
	select {
	case line := <-c.output:
		matches := rePid.FindStringSubmatch(line)
		log.Print(matches)
		if len(matches) == 0 {
			return 0, ErrNoMatch
		}
		pid, err := strconv.ParseInt(matches[1], 10, 32)
		if err != nil {
			return 0, err
		}
		return int(pid), nil
	case <-time.After(time.Duration(1) * time.Second):
		return 0, ErrReadTimeout
	case <-c.closed:
		return 0, ErrClosed
	}
}

func (c *defaultController) GetLogs() (string, error) {
	if c.conn == nil {
		return "", ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	if _, err := c.conn.Write([]byte("log all\n")); err != nil {
		return "", err
	}

	// read output
	var logs []string
	for {
		select {
		case line := <-c.output:
			if strings.HasPrefix(line, "END") {
				return strings.Join(logs, "\n"), nil
			}
			logs = append(logs, line)
			continue
		case <-time.After(time.Duration(1000) * time.Millisecond):
			return "", ErrReadTimeout
		case <-c.closed:
			return "", ErrClosed
		}
	}
}

func (c *defaultController) GetStates() ([]State, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	if _, err := c.conn.Write([]byte("state all\n")); err != nil {
		return nil, err
	}

	// read output
	var states []State
	for {
		select {
		case line := <-c.output:
			if line == "END" {
				return states, nil
			}
			st, err := ParseState(line)
			if err != nil {
				return nil, err
			}
			states = append(states, st)
		case <-time.After(time.Duration(100) * time.Millisecond):
			return nil, ErrReadTimeout
		case <-c.closed:
			return nil, ErrClosed
		}
	}
}

func (c *defaultController) GetStatus() (Status, error) {
	if c.conn == nil {
		return Status{}, ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	if _, err := c.conn.Write([]byte("status\n")); err != nil {
		return Status{}, err
	}

	// read output
	var status Status

	var parseClient = false
	var parseRoute = false
	var parseGlobalStats = false

	var clients []Client
	var routing []Route
	var stats GlobalStats

	for {
		select {
		case line := <-c.output:
			if line == "END" {
				status.GlobalStats = stats
				status.Clients = clients
				status.Routing = routing
				return status, nil
			}

			if line == "Common Name,Real Address,Bytes Received,Bytes Sent,Connected Since" {
				parseClient = true
				continue
			}

			if line == "ROUTING TABLE" {
				parseClient = false
			}

			if line == "Virtual Address,Common Name,Real Address,Last Ref" {
				parseRoute = true
				continue
			}

			if line == "GLOBAL STATS" {
				parseRoute = false
				parseGlobalStats = true
				continue
			}

			if parseClient {
				client, err := ParseClient(line)
				if err != nil {
					return status, err
				}

				clients = append(clients, client)
				continue
			}

			if parseRoute {
				route, err := ParseRoute(line)
				if err != nil {
					return status, err
				}

				routing = append(routing, route)
				continue
			}

			if parseGlobalStats {
				if strings.HasPrefix(line, "Max bcast/mcast queue length") {
					fields := strings.Split(line, ",")
					if len(fields) < 2 {
						continue
					}

					stats.Queue = strings.Trim(fields[1], " ")
				}
			}

		case <-time.After(time.Duration(100) * time.Millisecond):
			return Status{}, ErrReadTimeout
		case <-c.closed:
			return Status{}, ErrClosed
		}
	}
}

func (c *defaultController) GetClients() ([]Client, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	if _, err := c.conn.Write([]byte("status\n")); err != nil {
		return nil, err
	}

	// read output
	var clients []Client
	var startParse = false
	for {
		select {
		case line := <-c.output:
			if line == "ROUTING TABLE" {
				return clients, nil
			}

			if line == "Common Name,Real Address,Bytes Received,Bytes Sent,Connected Since" {
				startParse = true
				continue
			}

			if !startParse {
				continue
			}

			client, err := ParseClient(line)
			if err != nil {
				return nil, err
			}
			clients = append(clients, client)
		case <-time.After(time.Duration(100) * time.Millisecond):
			return nil, ErrReadTimeout
		case <-c.closed:
			return nil, ErrClosed
		}
	}
}

func (c *defaultController) GetRouting() ([]Route, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	if _, err := c.conn.Write([]byte("status\n")); err != nil {
		return nil, err
	}

	// read output
	var routing []Route
	var startParse = false
	for {
		select {
		case line := <-c.output:
			if line == "GLOBAL STATS" {
				return routing, nil
			}

			if line == "Virtual Address,Common Name,Real Address,Last Ref" {
				startParse = true
				continue
			}

			if !startParse {
				continue
			}

			route, err := ParseRoute(line)
			if err != nil {
				return nil, err
			}
			routing = append(routing, route)
		case <-time.After(time.Duration(100) * time.Second):
			return nil, ErrReadTimeout
		case <-c.closed:
			return nil, ErrClosed
		}
	}
}

func (c *defaultController) GetGlobalStats() (GlobalStats, error) {
	if c.conn == nil {
		return GlobalStats{}, ErrNotConnected
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// send command
	if _, err := c.conn.Write([]byte("status\n")); err != nil {
		return GlobalStats{}, err
	}

	// read output
	var stats GlobalStats
	var startParse bool
	for {
		select {
		case line := <-c.output:

			if line == "END" {
				return stats, nil
			}

			if line == "GLOBAL STATS" {
				startParse = true
				continue
			}

			if !startParse {
				continue
			}

			if strings.HasPrefix(line, "Max bcast/mcast queue length") {
				fields := strings.Split(line, ",")
				if len(fields) < 2 {
					continue
				}

				stats.Queue = strings.Trim(fields[1], " ")
			}

		case <-time.After(time.Duration(100) * time.Millisecond):
			return GlobalStats{}, ErrReadTimeout
		case <-c.closed:
			return GlobalStats{}, ErrClosed
		}
	}
}

func (c *defaultController) SubscribeState(ch chan State) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.statsSubscribers = append(c.statsSubscribers, ch)

	// send command
	if _, err := c.conn.Write([]byte("state on\n")); err != nil {
		return err
	}

	// read output
	select {
	case line := <-c.output:
		if !strings.HasPrefix(line, "SUCCESS:") {
			return errors.New(line)
		}
	case <-time.After(time.Duration(100) * time.Millisecond):
		return ErrReadTimeout
	case <-c.closed:
		return ErrClosed
	}
	return nil
}

func (c *defaultController) SubscribeByteCount(chIn, chOut chan int64) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.byteInSubscribers = append(c.byteInSubscribers, chIn)
	c.byteOutSubscribers = append(c.byteOutSubscribers, chOut)

	// send command
	if _, err := c.conn.Write([]byte("bytecount 1\n")); err != nil {
		return err
	}

	// read output
	select {
	case line := <-c.output:
		if !strings.HasPrefix(line, "SUCCESS:") {
			return errors.New(line)
		}
	case <-time.After(time.Duration(100) * time.Millisecond):
		return ErrReadTimeout
	case <-c.closed:
		return ErrClosed
	}
	return nil
}

func (c *defaultController) SubscribeLog(ch chan string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.logSubscribers = append(c.logSubscribers, ch)

	// send command
	if _, err := c.conn.Write([]byte("log on\n")); err != nil {
		return err
	}

	// read output
	select {
	case line := <-c.output:
		if !strings.HasPrefix(line, "SUCCESS:") {
			return errors.New(line)
		}
	case <-time.After(time.Duration(100) * time.Millisecond):
		return ErrReadTimeout
	case <-c.closed:
		return ErrClosed
	}
	return nil
}

func (c *defaultController) close() {
	c.closed <- true
	close(c.output)
}

func (c *defaultController) listen() {
	rd := bufio.NewReader(c.conn)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			c.close()
			return
		}
		line = strings.Trim(line, "\n\r")

		if strings.HasPrefix(line, ">INFO:") {
			// ignore all INFO response
			continue
		}
		if strings.HasPrefix(line, ">BYTECOUNT:") {
			// process byte count
			if len(c.byteInSubscribers) == 0 || len(c.byteOutSubscribers) == 0 {
				continue
			}
			nums := strings.Split(line[len(">BYTECOUNT: "):], ",")
			if len(nums) > 1 {
				bytesIn, err := strconv.ParseInt(nums[0], 10, 64)
				if err != nil {
					continue
				}
				bytesOut, err := strconv.ParseInt(nums[1], 10, 64)
				if err != nil {
					continue
				}
				for _, ch := range c.byteInSubscribers {
					ch <- bytesIn
				}
				for _, ch := range c.byteOutSubscribers {
					ch <- bytesOut
				}
			}
			continue
		}
		if strings.HasPrefix(line, ">STATE:") {
			// handle state
			if len(c.statsSubscribers) == 0 {
				continue
			}
			st, err := ParseState(line[len(">STATE: "):])
			if err == nil {
				for _, ch := range c.statsSubscribers {
					ch <- st
				}
			}
			continue
		}
		if strings.HasPrefix(line, ">LOG:") {
			// handle logs
			if len(c.logSubscribers) == 0 {
				continue
			}
			l := line[len(">LOG: "):]
			for _, ch := range c.logSubscribers {
				ch <- l
			}
			continue
		}
		// add to output
		c.output <- line
	}
}
