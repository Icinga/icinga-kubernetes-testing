package main

import (
	"context"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
	"os"
	"runtime"
	"strings"
	"time"
)

const prefix = "IK_TEST_"

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

	g, ctx := errgroup.WithContext(context.Background())

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, prefix) {
			switch env {
			case "IK_TEST_CPU":
				g.Go(func() error {
					return startCpuTest(ctx)
				})
			case "IK_TEST_MEMORY":
				g.Go(func() error {
					return startMemoryTest(ctx)
				})
			}
		}
	}

	if err := g.Wait(); err != nil {
		klog.Fatal(err)
	}

	klog.Info("Exiting")
}
