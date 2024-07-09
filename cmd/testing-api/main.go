package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/icinga/icinga-kubernetes-testing/pkg/contracts"
	schemav1 "github.com/icinga/icinga-kubernetes/pkg/schema/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	icingav1 "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/v1"
	icingav1client "github.com/icinga/icinga-kubernetes-testing/pkg/generated/clientset/versioned"
)

const (
	letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
)

func randString(length int) string {
	var result []byte
	for i := 0; i < length; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letterBytes))))
		result = append(result, letterBytes[num.Int64()])
	}
	return string(result)
}

func getClientset() (*kubernetes.Clientset, error) {
	kconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, errors.Wrap(err, "Can't configure Kubernetes client")
	}

	clientset, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return nil, errors.Wrap(err, "Can't create Kubernetes client")
	}

	return clientset, nil
}

func getIcingaClientset() (*icingav1client.Clientset, error) {
	kconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, errors.Wrap(err, "Can't configure Kubernetes client")
	}

	icingaClientset, err := icingav1client.NewForConfig(kconfig)

	return icingaClientset, nil
}

func wipeTests(ctx context.Context, icingaClientset *icingav1client.Clientset, namespace string) error {
	err := icingaClientset.IcingaV1().Tests(namespace).DeleteCollection(
		ctx,
		metav1.DeleteOptions{},
		metav1.ListOptions{},
	)
	if err != nil {
		return errors.Wrap(err, "Can't delete tests")
	}

	return nil
}

func cleanSpace(
	ctx context.Context,
	icingaClientset *icingav1client.Clientset,
	namespace string,
) error {
	if err := wipeTests(ctx, icingaClientset, namespace); err != nil {
		return err
	}

	return nil
}

func main() {
	clientset, err := getClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't get Kubernetes clientset"))
	}

	icingaClientset, err := getIcingaClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't get Icinga clientset"))
	}

	ctx := context.Background()

	db, err := sql.Open("mysql", "testing:testing@tcp(172.18.0.2)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't connect to database"))
	}
	defer db.Close()

	if err = cleanSpace(ctx, icingaClientset, contracts.TestingNamespace); err != nil {
		klog.Fatal(errors.Wrap(err, "Can't clean space"))
	}

	http.HandleFunc("/manage/wipe", wipePods(clientset, db, contracts.TestingNamespace))
	http.HandleFunc("/manage/delete", deletePods(clientset, db))

	http.HandleFunc("/test/delete", deleteTests(ctx, icingaClientset))
	http.HandleFunc("/test/create", createTest(ctx, icingaClientset, contracts.TestingNamespace))

	klog.Info("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		klog.Fatalf("Could not start server: %s\n", err.Error())
	}
}

func wipePods(
	clientset *kubernetes.Clientset,
	db *sql.DB,
	namespace string,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: contracts.TestingLabel,
		})
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't list pods")
			klog.Error(errors.Wrap(err, "Can't list pods"))
			return
		}

		counter := 0

		for _, pod := range pods.Items {
			currentPod, err := clientset.CoreV1().Pods(namespace).Get(
				context.Background(),
				pod.Name,
				metav1.GetOptions{},
			)
			err = clientset.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete pod %s", pod.GetName()))
				klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete pod %s", pod.GetName())))
				return
			} else {
				counter++

				_, err = db.Exec(
					"DELETE FROM pod_test WHERE pod_uuid = ?",
					schemav1.EnsureUUID(currentPod.GetUID()),
				)
				if err != nil {
					_, _ = fmt.Fprintln(
						w,
						fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName()),
					)
					klog.Error(
						errors.Wrap(
							err,
							fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName()),
						),
					)
					return
				}

				_, err = db.Exec(
					"DELETE FROM pod WHERE uuid = ?",
					schemav1.EnsureUUID(currentPod.GetUID()),
				)
				if err != nil {
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete pod %s from database", pod.GetName()))
					klog.Error(
						errors.Wrap(err, fmt.Sprintf("Can't delete pod %s from database", pod.GetName())),
					)
					return
				}
			}
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("%d Pods wiped", counter))
	}
}

func deletePods(clientset *kubernetes.Clientset, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		uuids := strings.Split(r.URL.Query().Get("uuids"), ",")
		counter := 0

		for _, uuid := range uuids {
			podUuid := schemav1.EnsureUUID(ktypes.UID(uuid))
			res, err := db.Query(
				"SELECT namespace, name FROM pod WHERE uuid = ?",
				podUuid,
			)
			if err != nil {
				_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't query pod with uuid %s", uuid))
				klog.Error(errors.Wrap(err, fmt.Sprintf("Can't query pod with uuid %s", uuid)))
				return
			}

			if !res.Next() {
				_, _ = fmt.Fprintln(w, fmt.Sprintf("Pod with uuid %s does not exist", uuid))
				klog.Error(errors.New(fmt.Sprintf("Pod with uuid %s does not exist", uuid)))
				return
			}

			var namespace, name string
			err = res.Scan(&namespace, &name)

			if strings.Contains(name, "icinga-for-testing-testing-api") {
				continue
			}
			pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
			err = clientset.CoreV1().Pods(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
			if err != nil {
				_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete pod %s", name))
				klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete pod %s", name)))
				return
			} else {
				counter++

				_, err = db.Exec(
					"DELETE FROM pod_test WHERE pod_uuid = ?",
					podUuid,
				)
				if err != nil {
					_, _ = fmt.Fprintln(
						w,
						fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName()),
					)
					klog.Error(
						errors.Wrap(
							err,
							fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName()),
						),
					)
					return
				}

				_, err = db.Exec(
					"DELETE FROM pod WHERE uuid = ?",
					podUuid,
				)
				if err != nil {
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete pod %s from database", pod.GetName()))
					klog.Error(
						errors.Wrap(err, fmt.Sprintf("Can't delete pod %s from database", pod.GetName())),
					)
					return
				}
			}
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("%d Pods deleted", counter))
	}
}

func deleteTests(
	ctx context.Context,
	icingaClientset *icingav1client.Clientset,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		klog.Info("Connection from " + r.RemoteAddr + " to " + r.URL.Path)

		testsParam := r.URL.Query().Get("tests")
		if testsParam == "" {
			_, _ = fmt.Fprintln(w, "No tests specified")
			return
		}

		testsToDelete := strings.Split(testsParam, ",")
		testsToDeletePerNs := make(map[string][]string)
		skipNamespaces := []string{}

		for _, test := range testsToDelete {
			split := strings.Split(test, "/")
			namespace, name := split[0], split[1]

			if slices.Contains(skipNamespaces, namespace) {
				continue
			}

			testsToDeletePerNs[namespace] = append(testsToDeletePerNs[namespace], name)

			if name == "*" {
				testsToDeletePerNs[namespace] = []string{"*"}
				skipNamespaces = append(skipNamespaces, namespace)
			}
		}

		for namespace, tests := range testsToDeletePerNs {
			if tests[0] == "*" {
				err := icingaClientset.IcingaV1().Tests(namespace).DeleteCollection(
					ctx,
					metav1.DeleteOptions{},
					metav1.ListOptions{},
				)
				if err != nil {
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete tests in namespace %s", namespace))
					klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete tests in namespace %s", namespace)))
					return
				}
			} else {
				for _, test := range tests {
					err := icingaClientset.IcingaV1().Tests(namespace).Delete(ctx, test, metav1.DeleteOptions{})
					if err != nil {
						_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete test %s in namespace %s", test, namespace))
						klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete test %s in namespace %s", test, namespace)))
						return
					}
				}
			}
		}
	}
}

func createTest(
	ctx context.Context,
	icingaClientset *icingav1client.Clientset,
	namespace string,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		klog.Info("Connection from " + r.RemoteAddr + " to " + r.URL.Path)

		deploymentName := r.URL.Query().Get("deploymentName")
		tests := strings.Split(r.URL.Query().Get("tests"), ":")
		if len(tests) == 1 && tests[0] == "" {
			_, _ = fmt.Fprintln(w, "No tests specified")
			return
		}

		var testResource *icingav1.Test

		testResource = &icingav1.Test{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "icinga-for-kubernetes-testing-test-" + randString(10),
				Namespace: namespace,
			},
			Spec: icingav1.TestSpec{
				DeploymentName: deploymentName,
			},
		}

		for _, test := range tests {
			testKind := strings.Split(test, ",")[0]
			totalReplicas, _ := strconv.Atoi(strings.Split(test, ",")[1])
			totalReplicas32 := int32(totalReplicas)

			badReplicas, _ := strconv.Atoi(strings.Split(test, ",")[2])
			badReplicas32 := int32(badReplicas)

			testResource.Spec.Tests = append(
				testResource.Spec.Tests,
				icingav1.TestTest{
					TestKind:      testKind,
					TotalReplicas: &totalReplicas32,
					BadReplicas:   &badReplicas32,
				},
			)
		}

		_, err := icingaClientset.IcingaV1().Tests(namespace).Create(ctx, testResource, metav1.CreateOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't create test %s", testResource.GetName()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't create test %s", testResource.GetName())))
			return
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("Created test %s", testResource.GetName()))
		klog.Info(fmt.Sprintf("Created test %s", testResource.GetName()))
	}
}
