// Copyright 2026 Paul Greenberg greenpau@outlook.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/greenpau/tested/pkg/app"
	"github.com/greenpau/tested/pkg/runner"
)

var (
	appVersion = "dev"
	gitBranch  = "unknown"
	gitCommit  = "unknown"
	buildUser  = "unknown"
	buildDate  = "unknown"
)

func main() {
	os.Exit(runProcess(os.Args[1:], os.Stdout, os.Stderr))
}

func runProcess(args []string, stdout, stderr *os.File) int {
	notifications := make(chan os.Signal, 1)
	signal.Notify(
		notifications,
		os.Interrupt,
		syscall.SIGTERM,
	)
	ctx, cancel := contextWithSignalCause(context.Background(), notifications)
	defer func() {
		signal.Stop(notifications)
		cancel()
	}()
	return app.Execute(ctx, args, stdout, stderr, app.BuildInfo{
		Version:   appVersion,
		GitBranch: gitBranch,
		GitCommit: gitCommit,
		BuildUser: buildUser,
		BuildDate: buildDate,
	})
}

func contextWithSignalCause(
	parent context.Context,
	notifications <-chan os.Signal,
) (context.Context, context.CancelFunc) {
	ctx, cancelCause := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case received, ok := <-notifications:
			if !ok {
				cancelCause(context.Canceled)
				return
			}
			cancelCause(runner.NewSignalCause(received))
		case <-ctx.Done():
		}
	}()
	cancel := func() {
		cancelCause(context.Canceled)
		<-done
	}
	return ctx, cancel
}
