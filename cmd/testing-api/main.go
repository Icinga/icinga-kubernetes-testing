package main

import (
	_ "github.com/go-sql-driver/mysql"

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	icingaClientset, err := getIcingaClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't get Icinga clientset"))
	}

	ctx := context.Background()

	db, err := sql.Open("mysql", "testing:testing@tcp(192.168.49.2:30003)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't connect to database"))
	}
	defer db.Close()

	if err = cleanSpace(ctx, icingaClientset, contracts.TestingNamespace); err != nil {
		klog.Fatal(errors.Wrap(err, "Can't clean space"))
	}

	http.HandleFunc("/test/delete", deleteTests(ctx, icingaClientset))
	http.HandleFunc("/test/create", createTest(ctx, db, icingaClientset, contracts.TestingNamespace))

	klog.Info("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		klog.Fatalf("Could not start server: %s\n", err.Error())
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

				_, _ = fmt.Fprintln(w, fmt.Sprintf("Deleted all tests int %s namespapce", namespace))
				klog.Info(fmt.Sprintf("Deleted all tests in %s namespace", namespace))
			} else {
				for _, test := range tests {
					err := icingaClientset.IcingaV1().Tests(namespace).Delete(ctx, test, metav1.DeleteOptions{})
					if err != nil {
						_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete test %s in namespace %s", test, namespace))
						klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete test %s in namespace %s", test, namespace)))
						return
					}

					_, _ = fmt.Fprintln(w, fmt.Sprintf("Deleted test %s", test))
					klog.Info(fmt.Sprintf("Deleted test %s", test))
				}
			}
		}
	}
}

func createTest(
	ctx context.Context,
	db *sql.DB,
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

		deploymentNames, err := db.Query("SELECT deployment_name FROM test")
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't get deployment names from database")
			klog.Error(errors.Wrap(err, "Can't get deployment names from database"))
			return
		}
		defer deploymentNames.Close()

		for deploymentNames.Next() {
			var name string
			_ = deploymentNames.Scan(&name)

			if name == deploymentName {
				_, _ = fmt.Fprintln(w, fmt.Sprintf("Deployment %s is already in use", deploymentName))
				klog.Error(errors.New(fmt.Sprintf("Deployment %s is already in use", deploymentName)))
				return
			}
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

			if totalReplicas32 < badReplicas32 {
				_, _ = fmt.Fprintln(
					w,
					fmt.Sprintf(
						"Bad replicas count %d is greater than total replicas count %d",
						badReplicas32,
						totalReplicas32,
					),
				)
				klog.Error(
					errors.New(
						fmt.Sprintf(
							"Bad replicas count %d is greater than total replicas count %d",
							badReplicas32,
							totalReplicas32,
						),
					),
				)
				return
			}

			testResource.Spec.Tests = append(
				testResource.Spec.Tests,
				icingav1.TestTest{
					TestKind:      testKind,
					TotalReplicas: &totalReplicas32,
					BadReplicas:   &badReplicas32,
				},
			)
		}

		_, err = icingaClientset.IcingaV1().Tests(namespace).Create(ctx, testResource, metav1.CreateOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't create test %s", testResource.GetName()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't create test %s", testResource.GetName())))
			return
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("Created test %s", testResource.GetName()))
		klog.Info(fmt.Sprintf("Created test %s", testResource.GetName()))
	}
}
