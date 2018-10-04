// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build linux freebsd netbsd openbsd solaris dragonfly darwin

package config

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func init() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGPIPE)

	go func() {
		for s := range c {
			fmt.Println("Got Signal:", s)
		}
	}()
}

// GetSyslogURI returns the configured/default syslog uri
func GetSyslogURI() string {
	enabled := Datadog.GetBool("log_to_syslog")
	uri := Datadog.GetString("syslog_uri")

	if enabled {
		if uri == "" {
			uri = defaultSyslogURI
		}
	}

	return uri
}
