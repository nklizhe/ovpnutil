// +build integration

package ovpnutil

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestKillOVPN(t *testing.T) {
	// TODO: start an openvpn process

	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	err2 := ctl.Signal(SIGTERM)
	assert.NoError(t, err2)
}

func TestGetpid(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	pid, err := ctl.Getpid()
	assert.NoError(t, err)
	assert.True(t, pid > 0)
}

func TestGetLogs(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	logs, err := ctl.GetLogs()
	assert.NoError(t, err)
	assert.NotEmpty(t, logs)

	fmt.Println(logs)
}

func TestGetStates(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	states, err := ctl.GetStates()
	assert.NoError(t, err)
	assert.NotEmpty(t, states)

	fmt.Println(states)
}

func TestGetStatus(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	status, err := ctl.GetStatus()
	assert.NoError(t, err)
	assert.NotEmpty(t, status)

	fmt.Println(status)
}

func TestGetClients(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	clients, err := ctl.GetClients()
	assert.NoError(t, err)
	assert.NotEmpty(t, clients)

	fmt.Println(clients)
}

func TestGetRouting(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	routing, err := ctl.GetRouting()
	assert.NoError(t, err)
	assert.NotEmpty(t, routing)

	fmt.Println(clients)
}

func TestGetGlobalStats(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	stats, err := ctl.GetGlobalStats()
	assert.NoError(t, err)
	assert.NotEmpty(t, stats)

	fmt.Println(stats)
}

func TestSubscribeByteCount(t *testing.T) {
	ctl, err := Dial("127.0.0.1:1337")
	assert.NoError(t, err)
	assert.NotNil(t, ctl)

	defer ctl.Close()

	cntIn := 0
	chIn := make(chan int64)
	cntOut := 0
	chOut := make(chan int64)

	// wait 5 seconds
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		err2 := ctl.SubscribeByteCount(chIn, chOut)
		assert.NoError(t, err2)

		timeout := time.After(time.Duration(3000) * time.Millisecond)
		for {
			select {
			case in := <-chIn:
				cntIn++
				fmt.Printf("in: %d\n", in)
				break
			case out := <-chOut:
				cntOut++
				fmt.Printf("out: %d\n", out)
				break
			case <-timeout:
				wg.Done()
				break
			}
		}
	}()

	wg.Wait()
	assert.True(t, cntIn > 0)
	assert.True(t, cntOut > 0)
}
