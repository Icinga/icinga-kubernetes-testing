package main

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/icinga/icinga-go-library/types"
	schemav1 "github.com/icinga/icinga-kubernetes/pkg/schema/v1"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"os"
	"runtime"
	"time"
)

func getClientset() (*kubernetes.Clientset, error) {
	kconfig, err := kclientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		kclientcmd.NewDefaultClientConfigLoadingRules(), &kclientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, errors.Wrap(err, "Can't configure Kubernetes client")
	}

	clientset, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return nil, errors.Wrap(err, "Can't create Kubernetes client")
	}

	return clientset, nil
}

func getPodUuid(clientset *kubernetes.Clientset, podName string, podNamespace string) (types.UUID, error) {
	pod, err := clientset.CoreV1().Pods(podNamespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return types.UUID{}, errors.Wrap(err, "Can't get pod")
	}

	return schemav1.EnsureUUID(pod.UID), nil
}

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

func contains(slice []string, item string) bool {
	for _, a := range slice {
		if a == item {
			return true
		}
	}
	return false
}

func main() {
	clientset, err := getClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't get Kubernetes clientset"))
	}

	db, err := sql.Open("mysql", "testing:testing@tcp(icinga-kubernetes-testing-database-service:3306)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't connect to database"))
	}
	defer db.Close()

	for i := 0; ; i++ {
		if i >= 10 {
			klog.Fatal(errors.New("Can't connect to database"))
		}
		if err := db.Ping(); err == nil {
			break
		}
		klog.Info("Waiting for database to be ready")
		time.Sleep(2 * time.Second)
	}
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")

	if podName == "" || podNamespace == "" {
		klog.Fatal("POD_NAME or POD_NAMESPACE is not set")
	}

	uuid, err := getPodUuid(clientset, podName, podNamespace)
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't get pod UUID"))
	}

	g, ctx := errgroup.WithContext(context.Background())
	testStopChans := make(map[string]context.CancelFunc)

	g.Go(func() error {
		var currentTests []string

		for {
			rows, err := db.Query("SELECT test FROM pod_test WHERE pod_uuid = ?", uuid)
			if err != nil {
				klog.Fatal(errors.Wrap(err, fmt.Sprintf("Can't get tests for pod %s", podName)))
			}

			var test string
			var newTests []string

			for rows.Next() {
				err = rows.Scan(&test)
				if err != nil {
					klog.Fatal(errors.Wrap(err, "Can't scan row"))
				}

				newTests = append(newTests, test)
			}

			for _, currentTest := range currentTests {
				if !contains(newTests, currentTest) {
					if stopFunc, ok := testStopChans[currentTest]; ok {
						stopFunc()
						delete(testStopChans, currentTest)
					}
				}
			}

			for _, newTest := range newTests {
				if !contains(currentTests, newTest) {
					testCtx, cancel := context.WithCancel(context.Background())
					testStopChans[newTest] = cancel
					switch newTest {
					case "cpu":
						g.Go(func() error {
							if err := startCpuTest(testCtx); err != nil {
								klog.Error(errors.Wrap(err, fmt.Sprintf("Error running test %s", newTest)))
								return err
							}

							return nil
						})
					case "memory":
						g.Go(func() error {
							if err := startMemoryTest(testCtx); err != nil {
								klog.Error(errors.Wrap(err, fmt.Sprintf("Error running test %s", newTest)))
								return err
							}

							return nil
						})
					}
				}
			}

			currentTests = newTests

			time.Sleep(5 * time.Second)
		}
	})

	g.Go(func() error {
		<-ctx.Done()

		for _, cancel := range testStopChans {
			cancel()
		}

		return nil
	})

	//g.Go(func() error {
	//	for {
	//		for _, test := range activeTests {
	//			switch test {
	//			case "cpu":
	//				if err := startCpuTest(ctx); err != nil {
	//					return err
	//				}
	//			}
	//		}
	//	}
	//})

	if err := g.Wait(); err != nil {
		klog.Fatal(err)
	}

	klog.Info("Exiting")
}
