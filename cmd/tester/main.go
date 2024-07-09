package main

import (
	"bufio"
	"context"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
	"net"
	"runtime"
	"time"
)

func startCpuTest(ctx context.Context) error {
	klog.Info("Starting cpu test")

	g, ctx := errgroup.WithContext(ctx)

	numCPU := runtime.NumCPU()
	for i := 0; i < numCPU; i++ {
		g.Go(func() error {
			for {
				//_ = math.Sin(math.Pi)
				klog.Info("CPU TEST")

				select {
				case <-ctx.Done():
					klog.Info("Stopping cpu test")
					return ctx.Err()
				case <-time.After(1 * time.Second):
					//case <-time.After(200 * time.Nanosecond):
				}
			}
		})
	}

	return g.Wait()
}

func startMemoryTest(ctx context.Context) error {
	klog.Info("Starting memory test")

	//mem := make([]byte, 0)

	for {
		//mem = append(mem, make([]byte, 200*1024*1024)...) // Allocate 100MB
		klog.Info("MEMORY TEST")

		select {
		case <-ctx.Done():
			klog.Info("Stopping memory test")
			return ctx.Err()
		case <-time.After(1 * time.Second):
			//case <-time.After(500 * time.Millisecond):
		}
	}
}

func main() {
	port := "8080"
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		klog.Error(errors.Wrap(err, "Failed to listen on port 8080"))
	}
	defer listener.Close()

	klog.Info(fmt.Sprintf("Listening on port %s", port))

	for {
		conn, err := listener.Accept()
		if err != nil {
			klog.Error(errors.Wrap(err, "Failed to accept connection"))
			continue
		}
		go func() {
			defer conn.Close()
			reader := bufio.NewReader(conn)
			for {
				message, err := reader.ReadString('\n')
				if err != nil {
					klog.Error(errors.Wrap(err, "Failed to read message"))
					return
				}
				klog.Info(fmt.Sprintf("Received YAML: %s", message))
			}
		}()
	}
}
