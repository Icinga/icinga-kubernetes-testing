package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"github.com/icinga/icinga-go-library/types"
	icingav1client "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/clientset/versioned"
	icingav1 "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/v1"
	"github.com/icinga/icinga-kubernetes-testing/pkg/contracts"
	schemav1 "github.com/icinga/icinga-kubernetes/pkg/schema/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
	prefix      = "IK_TEST_"
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

func getIcingaClientset() (*icingav1client.Clientset, error) {
	kconfig, err := kclientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		kclientcmd.NewDefaultClientConfigLoadingRules(), &kclientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, errors.Wrap(err, "Can't configure Kubernetes client")
	}

	icingaClientset, err := icingav1client.NewForConfig(kconfig)

	return icingaClientset, nil
}

func checkTestExists(db *sql.DB, uuid types.UUID, test string) (bool, error) {
	rows, err := db.Query(
		"SELECT * FROM pod_test WHERE pod_uuid = ? AND test = ?",
		uuid,
		test,
	)
	if err != nil {
		return false, errors.Wrap(err, "Can't execute query")
	}

	return rows.Next(), nil
}

func checkPodExists(db *sql.DB, uuid types.UUID) (bool, error) {
	rows, err := db.Query(
		"SELECT * FROM pod WHERE uuid = ?",
		uuid,
	)
	if err != nil {
		return false, errors.Wrap(err, "Can't execute query")
	}

	return rows.Next(), nil
}

func registerTest(db *sql.DB, uuid types.UUID, test string) error {
	_, err := db.Exec(
		"INSERT INTO pod_test (pod_uuid, test) VALUES (?, ?)",
		uuid,
		test,
	)
	if err != nil {
		return errors.Wrap(err, "Can't execute insert query")
	}

	return nil
}

func unregisterTest(db *sql.DB, uuid types.UUID, test string) error {
	_, err := db.Exec(
		"DELETE FROM pod_test WHERE pod_uuid = ? AND test = ?",
		uuid,
		test,
	)
	if err != nil {
		return errors.Wrap(err, "Can't execute delete query")
	}

	return nil
}

func deleteTesterPods(clientset *kubernetes.Clientset, namespace string) error {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: contracts.TestingLabel,
	})
	if err != nil {
		return errors.Wrap(err, "Can't list pods")
	}

	for _, pod := range pods.Items {
		err = clientset.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't delete pod %s", pod.GetName()))
		}
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

	//newTest := &icingav1.Test{
	//	ObjectMeta: metav1.ObjectMeta{
	//		Name:      "example-test",
	//		Namespace: "testing",
	//	},
	//	Spec: icingav1.TestSpec{
	//		CronSpec: "*/1 * * * *",
	//		Image:    "nginx:latest",
	//		Replicas: 3,
	//	},
	//}
	//
	//result, err := icingaClientset.IcingaV1().Tests("testing").Create(context.Background(), newTest, metav1.CreateOptions{})
	//if err != nil {
	//	klog.Fatal(errors.Wrap(err, "Can't create custom resource test"))
	//}
	//klog.Infof("Created custom resource test %s", result.GetName())

	//os.Exit(0)

	db, err := sql.Open("mysql", "testing:testing@tcp(icinga-for-kubernetes-testing-database-service:3306)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't connect to database"))
	}
	defer db.Close()

	namespace := "testing"

	if err = deleteTesterPods(clientset, namespace); err != nil {
		klog.Fatal(errors.Wrap(err, "Can't clean space"))
	}

	http.HandleFunc("/manage/create", createPods(clientset, db, namespace))
	http.HandleFunc("/manage/wipe", wipePods(clientset, db, namespace))
	http.HandleFunc("/manage/delete", deletePods(clientset, db))

	http.HandleFunc("/test/create", createTest(clientset, icingaClientset, namespace))

	klog.Info("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		klog.Fatalf("Could not start server: %s\n", err.Error())
	}
}

func createPods(clientset *kubernetes.Clientset, db *sql.DB, namespace string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		nParam := r.URL.Query().Get("n")
		if nParam == "" {
			nParam = "0"
		}

		n, err := strconv.Atoi(nParam)
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't parse parameter \"n\"")
			klog.Error(errors.Wrap(err, "Can't parse parameter \"n\""))
			return
		}

		requestCpu := r.URL.Query().Get("requestCpu")
		requestMemory := r.URL.Query().Get("requestMemory")
		limitCpu := r.URL.Query().Get("limitCpu")
		limitMemory := r.URL.Query().Get("limitMemory")

		_, _ = fmt.Fprintln(w, requestCpu, requestMemory, limitCpu, limitMemory)

		data, err := os.ReadFile("tester.yml")
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't read tester resource file")
			klog.Error(errors.Wrap(err, "Can't read tester resource file"))
			return
		}

		var pod corev1.Pod
		err = yaml.Unmarshal(data, &pod)
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't unmarshal tester resource yaml")
			klog.Error(errors.Wrap(err, "Can't unmarshal tester resource yaml"))
			return
		}

		for i := 0; i < n; i++ {
			currentPod := pod
			currentPod.ObjectMeta.Name += "-" + randString(10)

			if requestCpu != "" {
				currentPod.Spec.Containers[0].Resources.Requests["cpu"] = resource.MustParse(requestCpu)
			}
			if requestMemory != "" {
				currentPod.Spec.Containers[0].Resources.Requests["memory"] = resource.MustParse(requestMemory)
			}
			if limitCpu != "" {
				currentPod.Spec.Containers[0].Resources.Limits["cpu"] = resource.MustParse(limitCpu)
			}
			if limitMemory != "" {
				currentPod.Spec.Containers[0].Resources.Limits["memory"] = resource.MustParse(limitMemory)
			}

			createdPod, err := clientset.CoreV1().Pods(namespace).Create(context.Background(), &currentPod, metav1.CreateOptions{})
			if err != nil {
				_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't create pod %s", currentPod.GetName()))
				klog.Error(errors.Wrap(err, fmt.Sprintf("Can't create pod %s", currentPod.GetName())))
				return
			}

			_, err = db.Exec(
				"INSERT INTO pod (uuid, namespace, name) VALUES (?, ?, ?)",
				schemav1.EnsureUUID(createdPod.GetUID()),
				createdPod.GetNamespace(),
				createdPod.GetName(),
			)
			if err != nil {
				_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't insert pod %s into database", createdPod.GetName()))
				klog.Error(errors.Wrap(err, fmt.Sprintf("Can't insert pod %s into database", createdPod.GetName())))
				return
			}
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("%d Pods created", n))
	}
}

func wipePods(clientset *kubernetes.Clientset, db *sql.DB, namespace string) func(w http.ResponseWriter, r *http.Request) {
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
			currentPod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
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
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName()))
					klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName())))
					return
				}

				_, err = db.Exec(
					"DELETE FROM pod WHERE uuid = ?",
					schemav1.EnsureUUID(currentPod.GetUID()),
				)
				if err != nil {
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete pod %s from database", pod.GetName()))
					klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete pod %s from database", pod.GetName())))
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

			if strings.Contains(name, "icinga-for-testing-controller") {
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
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName()))
					klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete tests for pod %s from database", pod.GetName())))
					return
				}

				_, err = db.Exec(
					"DELETE FROM pod WHERE uuid = ?",
					podUuid,
				)
				if err != nil {
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete pod %s from database", pod.GetName()))
					klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete pod %s from database", pod.GetName())))
					return
				}
			}
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("%d Pods deleted", counter))
	}
}

func createTest(clientset *kubernetes.Clientset, icingaClientset *icingav1client.Clientset, namespace string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		requestCpu := r.URL.Query().Get("requestCpu")
		requestMemory := r.URL.Query().Get("requestMemory")
		limitCpu := r.URL.Query().Get("limitCpu")
		limitMemory := r.URL.Query().Get("limitMemory")
		tests := r.URL.Query().Get("tests")
		if tests == "" {
			_, _ = fmt.Fprintln(w, "No tests specified")
			return
		}

		//configMap := &corev1.ConfigMap{
		//	ObjectMeta: metav1.ObjectMeta{
		//		Name:      "tester-config",
		//		Namespace: "testing",
		//	},
		//}
		//
		//for _, test := range strings.Split(tests, ",") {
		//	configMap.Data[prefix+strings.ToUpper(test)] = "true"
		//}

		data, err := os.ReadFile("tester.yml")
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't read tester resource file")
			klog.Error(errors.Wrap(err, "Can't read tester resource file"))
			return
		}

		var testResource icingav1.Test
		err = yaml.Unmarshal(data, &testResource)
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't unmarshal tester resource yaml")
			klog.Error(errors.Wrap(err, "Can't unmarshal tester resource yaml"))
			return
		}

		testResource.ObjectMeta.Name += "-" + randString(10)

		if requestCpu != "" {
			testResource.Spec.Containers[0].Resources.Requests["cpu"] = resource.MustParse(requestCpu)
		}
		if requestMemory != "" {
			testResource.Spec.Containers[0].Resources.Requests["memory"] = resource.MustParse(requestMemory)
		}
		if limitCpu != "" {
			testResource.Spec.Containers[0].Resources.Limits["cpu"] = resource.MustParse(limitCpu)
		}
		if limitMemory != "" {
			testResource.Spec.Containers[0].Resources.Limits["memory"] = resource.MustParse(limitMemory)
		}

		_, err = icingaClientset.IcingaV1().Tests(namespace).Create(context.Background(), &testResource, metav1.CreateOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't create test %s", testResource.GetName()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't create test %s", testResource.GetName())))
			return
		}

		//_, err = db.Exec(
		//	"INSERT INTO pod (uuid, namespace, name) VALUES (?, ?, ?)",
		//	schemav1.EnsureUUID(createdTest.GetUID()),
		//	createdTest.GetNamespace(),
		//	createdTest.GetName(),
		//)
		//if err != nil {
		//	_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't insert test %s into database", createdTest.GetName()))
		//	klog.Error(errors.Wrap(err, fmt.Sprintf("Can't insert test %s into database", createdTest.GetName())))
		//	return
		//}
	}
}
