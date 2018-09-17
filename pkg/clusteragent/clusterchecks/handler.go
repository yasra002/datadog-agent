// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build clusterchecks

package clusterchecks

import (
	"errors"
	"sync"
	"time"

	"github.com/DataDog/datadog-agent/pkg/autodiscovery"
	"github.com/DataDog/datadog-agent/pkg/config"
)

const (
	schedulerName = "clusterchecks"
)

type state int

const (
	initState state = iota
	followerState
	warmupState
	activeState
)

// The handler is the glue holding all components for cluster-checks management
type Handler struct {
	m                sync.Mutex
	autoconfig       *autodiscovery.AutoConfig
	dispatcher       *dispatcher
	discoveryRunning bool
	leaderWatchStop  chan struct{}
	leaderName       string
	leaderIP         string
}

// SetupHandler returns a populated Handler
// If will hook on the specified AutoConfig instance at Start
func SetupHandler(ac *autodiscovery.AutoConfig) (*Handler, error) {
	h := &Handler{
		autoconfig: ac,
	}
	return h, nil
}

// Start
func (h *Handler) Start() error {
	if !config.Datadog.GetInt("leader_election") {
		// With no leader election, we assume only one DCA is running
		return h.startDiscovery()
	}

	h.m.Lock()
	defer h.m.Unlock()
	h.leaderWatchStop = make(chan struct{}, 1)
	h.
	go h.leaderWatch()
}

// Stop
func (h *Handler) Stop() error {
	h.stopDiscovery()

	// Stop leader watching goroutine if running
	h.m.RLock()
	defer h.m.RUnlock()
	if h.leaderWatchStop != nil {
		close(h.leaderWatchStop)
	}
}

// startDiscovery hooks to Autodiscovery and starts managing checks
func (h *Handler) startDiscovery() error {
	h.m.Lock()
	defer h.m.Unlock()
	if h.discoveryRunning {
		return errors.New("already running")
	}

	// Clean initial state
	h.dispatcher = newDispatcher()
	h.discoveryRunning = true


	// Register our scheduler and ask for a config replay
	h.autoconfig.AddScheduler(schedulerName, h.dispatcher, true)

	return nil
}

// stopDiscovery stops the management logic and un-hooks from Autodiscovery
func (h *Handler) stopDiscovery() error {
	h.m.Lock()
	defer h.m.Unlock()
	if !h.discoveryRunning {
		return errors.New("not running")
	}

	// AD will call dispatcher.Stop for us
	h.autoconfig.RemoveScheduler(schedulerName)

	// Release memory
	h.dispatcher = nil

	return nil
}

// leaderWatch
func (h *Handler) leaderWatch() {
	watchTicker := time.NewTicker(5 * time.Second)
	defer watchTicker.Stop()

	leaderState := initState

	for {
		select {
		case <-watchTicker:

		case <-h.leaderWatchStop:
			return
		}
	}
}

func (h *Handler) updateLeader(name string) {
	h.RLock()


}
