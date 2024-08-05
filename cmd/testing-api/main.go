package main

import (
	_ "github.com/go-sql-driver/mysql"
	"k8s.io/client-go/kubernetes"

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

// TODO remove const and use variable in the function instead

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

	// Add more resources to clean here if needed

	return nil
}

func main() {
	icingaClientset, err := getIcingaClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't get Icinga clientset"))
	}

	clientset, err := getClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't get Kubernetes clientset"))
	}

	ctx := context.Background()

	db, err := sql.Open("mysql", "testing:testing@tcp(192.168.49.2:30003)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't connect to database"))
	}
	defer func() { _ = db.Close() }()

	// Wipe all tests in the TestingNamespace
	if err = cleanSpace(ctx, icingaClientset, contracts.TestingNamespace); err != nil {
		klog.Fatal(errors.Wrap(err, "Can't clean space"))
	}

	http.HandleFunc("/test/delete", deleteTests(ctx, icingaClientset))
	http.HandleFunc("/test/create", createTest(ctx, db, clientset, icingaClientset, contracts.TestingNamespace))

	klog.Info("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		klog.Fatalf("Could not start server: %s\n", err.Error())
	}
}

// deleteTests deletes tests specified in the query parameter. The tests are
// specified as "namespace/testName" and separated by comma. If the namespace
// and test name are set to "*" all tests in all namespaces will be deleted.
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

		// Tests are specified as "namespace/testName" and separated by comma
		testsToDelete := strings.Split(testsParam, ",")
		testsToDeletePerNs := make(map[string][]string)
		var skipNamespaces []string

		for _, test := range testsToDelete {
			split := strings.Split(test, "/")
			namespace, name := split[0], split[1]

			// Skip the namespace if one test name in the namespace is set to "*"
			if slices.Contains(skipNamespaces, namespace) {
				continue
			}

			// Store the tests to delete in a map with the namespace as key
			testsToDeletePerNs[namespace] = append(testsToDeletePerNs[namespace], name)

			// If the test name is set to "*" the namespace will be skipped in future loop passes
			if name == "*" {
				testsToDeletePerNs[namespace] = []string{"*"}
				skipNamespaces = append(skipNamespaces, namespace)
			}
		}

		for namespace, tests := range testsToDeletePerNs {
			// If the test name is set to "*" all tests in the namespace will be deleted otherwise
			// the specified tests will be deleted
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

// TODO send http status codes -> w.WriteHeader(http.StatusInternalServerError)

// createTest builds a test resource out of the query parameters and deploys it to the cluster.
func createTest(
	ctx context.Context,
	db *sql.DB,
	clientset *kubernetes.Clientset,
	icingaClientset *icingav1client.Clientset,
	namespace string,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		klog.Info("Connection from " + r.RemoteAddr + " to " + r.URL.Path)

		resourceType := r.URL.Query().Get("resourceType")
		resourceName := r.URL.Query().Get("resourceName")
		description := r.URL.Query().Get("description")
		expectedPods := r.URL.Query().Get("expectedPods")
		tests := strings.Split(r.URL.Query().Get("tests"), ":")

		// Get resource name for specified resource type
		resourceNames, err := db.Query(
			"SELECT resource_name FROM test WHERE resource_type = ? AND resource_name = ?",
			resourceType,
			resourceName,
		)
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't get resource names from database")
			klog.Error(errors.Wrap(err, "Can't get resource names from database"))
			return
		}
		defer func() { _ = resourceNames.Close() }()

		resourceNames.Next()
		var name string

		_ = resourceNames.Scan(&name)

		// Check if resource name is already in use
		if name == resourceName {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("%s '%s' is already in use", resourceType, resourceName))
			klog.Error(errors.New(fmt.Sprintf("%s '%s' is already in use", resourceType, resourceName)))
			return
		}

		expectedPodsInt, _ := strconv.Atoi(expectedPods)

		// Some extra checks for DaemonSet
		if resourceType == "daemonset" {
			nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				_, _ = fmt.Fprintln(w, "Can't get nodes")
				klog.Error(errors.Wrap(err, "Can't get nodes"))
				return
			}

			// Check if there are enough nodes to run tests.
			// Exit if there are more tests than nodes.
			if len(nodes.Items) < len(tests) {
				_, _ = fmt.Fprintln(w, fmt.Sprintf(
					"Not enough nodes to run tests. Nodes: %d, Tests: %d",
					len(nodes.Items),
					len(tests),
				))
				klog.Error(errors.New(fmt.Sprintf(
					"Not enough nodes to run tests. Nodes: %d, Tests: %d",
					len(nodes.Items),
					len(tests),
				)))
				return
			}

			// Because expectedPods can't be set for DaemonSet in the
			// frontend it is set to the number of nodes by default.
			expectedPodsInt = len(nodes.Items)
		}

		// Build new test resource out of the query parameters
		testResource := &icingav1.Test{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "icinga-for-kubernetes-test-" + randString(8),
				Namespace: namespace,
			},
			Spec: icingav1.TestSpec{
				ResourceType: resourceType,
				ResourceName: resourceName,
				Description:  description,
				ExpectedPods: int32(expectedPodsInt),
			},
		}

		// Check if tests are specified and if so add them to Spec.Tests of the built test resource
		if tests[0] != "" {
			for _, test := range tests {
				testKind := strings.Split(test, ",")[0]
				testPercentage, _ := strconv.Atoi(strings.Split(test, ",")[1])

				if testPercentage < 1 || testPercentage > 100 {
					_, _ = fmt.Fprintln(
						w,
						fmt.Sprintf(
							"Test percentage has to be between 1 and 100! Currently is: %d",
							testPercentage,
						),
					)
					klog.Error(
						errors.New(
							fmt.Sprintf(
								"Test percentage has to be between 1 and 100! Currently is: %d",
								testPercentage,
							),
						),
					)
					return
				}

				testResource.Spec.Tests = append(
					testResource.Spec.Tests,
					icingav1.TestTest{
						TestKind:       testKind,
						TestPercentage: int32(testPercentage),
					},
				)
			}
		}

		// Deploy the built test resource to the cluster
		_, err = icingaClientset.IcingaV1().Tests(namespace).Create(ctx, testResource, metav1.CreateOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't create test %s", testResource.GetName()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't create test %s", testResource.GetName())))
			return
		}

		// TODO send 200 status code
		_, _ = fmt.Fprintln(w, fmt.Sprintf("Created test %s", testResource.GetName()))
		klog.Info(fmt.Sprintf("Created test %s", testResource.GetName()))
	}
}
