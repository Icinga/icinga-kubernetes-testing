package main

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/icinga/icinga-go-library/types"
	schemav1 "github.com/icinga/icinga-kubernetes/pkg/schema/v1"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"os"
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

	for {
		rows, err := db.Query("SELECT test FROM pod_test WHERE pod_uuid = ?", uuid)
		if err != nil {
			klog.Fatal(errors.Wrap(err, fmt.Sprintf("Can't get tests for pod %s", podName)))
		}

		var test string
		var tests []string

		for rows.Next() {
			err = rows.Scan(&test)
			if err != nil {
				klog.Fatal(errors.Wrap(err, "Can't scan row"))
			}
			klog.Info(fmt.Sprintf("%s: %s", uuid.String(), test))
			tests = append(tests, test)
		}

		time.Sleep(5 * time.Second)
	}
}
