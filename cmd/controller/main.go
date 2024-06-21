package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"github.com/icinga/icinga-go-library/types"
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

const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"

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

func checkTestExists(db *sql.DB, podUuid types.UUID, test string) (bool, error) {
	rows, err := db.Query(
		"SELECT * FROM pod_test WHERE pod_uuid = ? AND test = ?",
		podUuid,
		test,
	)
	if err != nil {
		return false, errors.Wrap(err, "Can't execute query")
	}

	return rows.Next(), nil
}

func checkPodExists(db *sql.DB, podUuid types.UUID) (bool, error) {
	rows, err := db.Query(
		"SELECT * FROM pod WHERE uuid = ?",
		podUuid,
	)
	if err != nil {
		return false, errors.Wrap(err, "Can't execute query")
	}

	return rows.Next(), nil
}

func registerTest(db *sql.DB, podUuid types.UUID, test string) error {
	_, err := db.Exec(
		"INSERT INTO pod_test (pod_uuid, test) VALUES (?, ?)",
		podUuid,
		test,
	)
	if err != nil {
		return errors.Wrap(err, "Can't execute insert query")
	}

	return nil
}

func unregisterTest(db *sql.DB, podUuid types.UUID, test string) error {
	_, err := db.Exec(
		"DELETE FROM pod_test WHERE pod_uuid = ? AND test = ?",
		podUuid,
		test,
	)
	if err != nil {
		return errors.Wrap(err, "Can't execute delete query")
	}

	return nil
}

func main() {
	clientset, err := getClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "can't get Kubernetes clientset"))
	}

	db, err := sql.Open("mysql", "testing:testing@tcp(icinga-kubernetes-testing-database-service:3306)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't connect to database"))
	}
	defer db.Close()

	namespace := "testing"

	http.HandleFunc("/manage/create", createPods(clientset, namespace, db))
	http.HandleFunc("/manage/wipe", wipePods(clientset, namespace, db))
	http.HandleFunc("/manage/delete", deletePods(clientset, db))

	http.HandleFunc("/test/start/cpu", startTestCpu(db))
	http.HandleFunc("/test/start/memory", startTestMemory(db))

	http.HandleFunc("/test/stop/cpu", stopTestCpu(db))
	http.HandleFunc("/test/stop/memory", stopTestMemory(db))

	klog.Info("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		klog.Fatalf("Could not start server: %s\n", err.Error())
	}
}

func createPods(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
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

func wipePods(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(w, "Can't list pods")
			klog.Error(errors.Wrap(err, "Can't list pods"))
			return
		}

		counter := 0

		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, "icinga-kubernetes-testing-tester") {
				currentPod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
				err = clientset.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
				if err != nil {
					_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't delete pod %s", pod.GetName()))
					klog.Error(errors.Wrap(err, fmt.Sprintf("Can't delete pod %s", pod.GetName())))
					return
				} else {
					counter++
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

			if strings.Contains(name, "icinga-kubernetes-testing-controller") {
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

func startTestCpu(db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		podUuid := schemav1.EnsureUUID(ktypes.UID(r.URL.Query().Get("uuid")))

		podExists, err := checkPodExists(db, podUuid)
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String())))
			return
		}

		if !podExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Pod uuid %s does not exist", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Pod uuid %s does not exist", podUuid.String())))
			return
		}

		testExists, err := checkTestExists(db, podUuid, "cpu")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if cpu test exists for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if cpu test exists for pod uuid %s", podUuid.String())))
			return
		}

		if testExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Cpu test already started for pod uuid %s", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Cpu test already started for pod uuid %s", podUuid.String())))
			return
		}

		err = registerTest(db, podUuid, "cpu")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't start cpu test for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't start cpu test for pod uuid %s", podUuid.String())))
			return
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("Cpu test started for pod uuid %s", podUuid.String()))
	}
}

func startTestMemory(db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		podUuid := schemav1.EnsureUUID(ktypes.UID(r.URL.Query().Get("uuid")))

		podExists, err := checkPodExists(db, podUuid)
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String())))
			return
		}

		if !podExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Pod uuid %s does not exist", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Pod uuid %s does not exist", podUuid.String())))
			return
		}

		testExists, err := checkTestExists(db, podUuid, "memory")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if memory test exists for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if memory test exists for pod uuid %s", podUuid.String())))
			return
		}

		if testExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Memory test already started for pod uuid %s", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Memory test already started for pod uuid %s", podUuid.String())))
			return
		}

		err = registerTest(db, podUuid, "memory")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't start memory test for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't start memory test for pod uuid %s", podUuid.String())))
			return
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("Memory test started for pod uuid %s", podUuid.String()))
	}
}

func stopTestCpu(db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		podUuid := schemav1.EnsureUUID(ktypes.UID(r.URL.Query().Get("uuid")))

		podExists, err := checkPodExists(db, podUuid)
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String())))
			return
		}

		if !podExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Pod uuid %s does not exist", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Pod uuid %s does not exist", podUuid.String())))
			return
		}

		testExists, err := checkTestExists(db, podUuid, "cpu")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if cpu test exists for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if cpu test exists for pod uuid %s", podUuid.String())))
			return
		}

		if !testExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Cpu test is not running for pod uuid %s", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Cpu test is not running for pod uuid %s", podUuid.String())))
			return
		}

		err = unregisterTest(db, podUuid, "cpu")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't stop cpu test for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't stop cpu test for pod uuid %s", podUuid.String())))
			return
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("Cpu test stopped for pod uuid %s", podUuid.String()))
	}
}

func stopTestMemory(db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		podUuid := schemav1.EnsureUUID(ktypes.UID(r.URL.Query().Get("uuid")))

		podExists, err := checkPodExists(db, podUuid)
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if pod uuid %s exists", podUuid.String())))
			return
		}

		if !podExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Pod uuid %s does not exist", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Pod uuid %s does not exist", podUuid.String())))
			return
		}

		testExists, err := checkTestExists(db, podUuid, "memory")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't check if memory test exists for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't check if memory test exists for pod uuid %s", podUuid.String())))
			return
		}

		if !testExists {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Memory test is not running for pod uuid %s", podUuid.String()))
			klog.Error(errors.New(fmt.Sprintf("Memory test is not running for pod uuid %s", podUuid.String())))
			return
		}

		err = unregisterTest(db, podUuid, "memory")
		if err != nil {
			_, _ = fmt.Fprintln(w, fmt.Sprintf("Can't stop memory test for pod uuid %s", podUuid.String()))
			klog.Error(errors.Wrap(err, fmt.Sprintf("Can't stop memory test for pod uuid %s", podUuid.String())))
			return
		}

		_, _ = fmt.Fprintln(w, fmt.Sprintf("Memory test stopped for pod uuid %s", podUuid.String()))
	}
}
