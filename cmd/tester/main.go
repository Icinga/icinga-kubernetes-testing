package main

import (
	"bufio"
	"context"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	"net"
	"strings"
	"time"
)

func startCpuTest(ctx context.Context) error {
	klog.Info("Starting cpu test")

	g, ctx := errgroup.WithContext(ctx)

	//numCpu := runtime.NumCPU()
	numCpu := 1
	for i := 0; i < numCpu; i++ {
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

type testConfig struct {
	Test string `json:"test"`
}

func main() {
	config := getConfigFromPort("8080")

	klog.Info("Config: ", config)

	ctx := context.Background()

	switch strings.Split(config.Test, ".")[0] {
	case "cpu":
		err := startCpuTest(ctx)
		if err != nil {
			klog.Error(errors.Wrap(err, "Failed to start CPU test"))
		}
	case "memory":
		err := startMemoryTest(ctx)
		if err != nil {
			klog.Error(errors.Wrap(err, "Failed to start memory test"))
		}
	default:
		klog.Error("Unknown test type")
	}

	stop := make(chan struct{})
	<-stop
}

func getConfigFromPort(port string) testConfig {
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		klog.Error(errors.Wrap(err, "Failed to listen on port 8080"))
	}
	defer func() { _ = listener.Close() }()

	klog.Info(fmt.Sprintf("Listening on port %s", port))

	for {
		conn, err := listener.Accept()
		if err != nil {
			klog.Error(errors.Wrap(err, "Failed to accept connection"))
			continue
		}
		defer func() { _ = conn.Close() }()

		klog.Info("Connection accepted")

		reader := bufio.NewReader(conn)
		message, err := reader.ReadString('\n')
		if err != nil {
			klog.Error(errors.Wrap(err, "Failed to read message"))
			continue
		}

		klog.Info(fmt.Sprintf("Received YAML: %s", message))

		var config testConfig
		err = yaml.Unmarshal([]byte(message), &config)
		if err != nil {
			klog.Error(errors.Wrap(err, "Failed to unmarshal YAML"))
			continue
		}

		if config.Test != "" {
			klog.Info("Field 'Test' found in YAML. Stopping listener.")
			return config
		}
	}
}
