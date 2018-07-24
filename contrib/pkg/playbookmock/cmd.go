/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package playbookmock

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type options struct {
	Port int
}

func NewPlaybookMockCommand() *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "playbook-mock",
		Short: "Mock playbook command for fake-openshift-ansible",
		Run: func(cmd *cobra.Command, args []string) {
			logger.Infof("args = %v", args)
			if err := opts.run(); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
				logger.Fatal(err)
			}
		},
	}
	flags := cmd.Flags()
	flags.IntVar(&opts.Port, "port", 8055, "use '--port' option to specify the port to listen on")
	return cmd
}

func (o *options) run() error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	cancelOnInterrupt(ctx, cancelFunc)

	return o.runWithContext(ctx)
}

func (o *options) runWithContext(ctx context.Context) error {
	addr := ":" + strconv.Itoa(o.Port)

	return Run(ctx, addr)
}

// cancelOnInterrupt calls f when os.Interrupt or SIGTERM is received.
// It ignores subsequent interrupts on purpose - program should exit correctly after the first signal.
func cancelOnInterrupt(ctx context.Context, f context.CancelFunc) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ctx.Done():
		case <-c:
			f()
		}
	}()
}
