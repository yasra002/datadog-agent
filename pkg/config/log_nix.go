// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build linux freebsd netbsd openbsd solaris dragonfly darwin

package config

import (
	"os"
	"os/signal"
	"syscall"
)

func init() {
	// By default systemd redirect the stdout to journald. When journald is stopped or crashes we receive a SIGPIPE signal.
	// Go ignore SIGPIPE signals unless it is when stdout or stdout is closed, in this case the agent is stopped. We don't want that
	// so we intercept the SIGPIPE signals and just discard them.
	// systemd 231+ set the $JOURNAL_STREAM env variable when it is redirecting stdout to journald.
	// for older version of systemd, a workaround is to use the syslog integration instead of relying on stdout
	if _, ok := os.LookupEnv("JOURNAL_STREAM"); ok {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGPIPE)
		go func() {
			for range c {
				// do nothing
			}
		}()
	}
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
