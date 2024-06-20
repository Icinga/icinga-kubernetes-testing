package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	schemav1 "github.com/icinga/icinga-kubernetes/pkg/schema/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	kclientcmd "k8s.io/client-go/tools/clientcmd"
	"log"
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
		return nil, errors.Wrap(err, "can't configure Kubernetes client")
	}

	clientset, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return nil, errors.Wrap(err, "can't create Kubernetes client")
	}

	return clientset, nil
}

func main() {
	clientset, err := getClientset()
	if err != nil {
		log.Fatal(errors.Wrap(err, "can't get Kubernetes clientset"))
	}

	db, err := sql.Open("mysql", "testing:testing@tcp(icinga-kubernetes-testing-database-service:3306)/testing")
	if err != nil {
		log.Fatal(errors.Wrap(err, "can't connect to database"))
	}
	defer db.Close()

	namespace := "testing"

	http.HandleFunc("/manage/create", createPods(clientset, namespace, db))
	http.HandleFunc("/manage/wipe", wipePods(clientset, namespace, db))
	http.HandleFunc("/manage/delete", deletePods(clientset, namespace, db))

	http.HandleFunc("/test/start/cpu", startTestCpu(clientset, namespace, db))
	http.HandleFunc("/test/start/memory", startTestMemory(clientset, namespace, db))

	http.HandleFunc("/test/stop/cpu", stopTestCpu(clientset, namespace, db))
	http.HandleFunc("/test/stop/memory", stopTestMemory(clientset, namespace, db))

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err.Error())
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
			log.Fatal(errors.Wrap(err, "can't parse parameter \"n\""))
		}

		requestCpu := r.URL.Query().Get("requestCpu")
		requestMemory := r.URL.Query().Get("requestMemory")
		limitCpu := r.URL.Query().Get("limitCpu")
		limitMemory := r.URL.Query().Get("limitMemory")

		fmt.Fprintln(w, requestCpu, requestMemory, limitCpu, limitMemory)

		data, err := os.ReadFile("tester.yml")
		if err != nil {
			log.Fatal(errors.Wrap(err, "can't read file"))
		}

		var pod corev1.Pod
		err = yaml.Unmarshal(data, &pod)
		if err != nil {
			log.Fatal(errors.Wrap(err, "error unmarshalling yaml"))
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
				log.Fatal(errors.Wrap(err, "can't create deployment"))
			}

			_, err = db.Exec(
				"INSERT INTO pod (uuid, namespace, name) VALUES (?, ?, ?)",
				schemav1.EnsureUUID(createdPod.GetUID()),
				createdPod.GetNamespace(),
				createdPod.GetName(),
			)
			if err != nil {
				log.Fatal(errors.Wrap(err, fmt.Sprintf("can't insert pod %s into database", createdPod.GetName())))
			}
		}

		fmt.Fprintln(w, fmt.Sprintf("%d Pods created", n))
	}
}

func wipePods(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			log.Fatal(errors.Wrap(err, "can't list pods"))
		}

		counter := 0

		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, "icinga-kubernetes-testing-tester") {
				currentPod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
				err = clientset.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
				if err != nil {
					log.Fatal(errors.Wrap(err, "can't delete pod"))
				} else {
					counter++
					_, err = db.Exec(
						"DELETE FROM pod WHERE uuid = ?",
						schemav1.EnsureUUID(currentPod.GetUID()),
					)
					if err != nil {
						log.Fatal(errors.Wrap(err, fmt.Sprintf("can't delete pod %s from database", pod.GetName())))
					}
				}
			}
		}

		fmt.Fprintln(w, fmt.Sprintf("%d Pods wiped", counter))
	}
}

func deletePods(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		namesParam := r.URL.Query().Get("names")
		names := strings.Split(namesParam, ",")
		counter := 0

		for _, name := range names {
			if strings.Contains(name, "icinga-kubernetes-testing-controller") {
				continue
			}
			pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
			err = clientset.CoreV1().Pods(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
			if err != nil {
				log.Fatal(errors.Wrap(err, "can't delete pod"))
			} else {
				counter++
				_, err = db.Exec(
					"DELETE FROM pod WHERE uuid = ?",
					schemav1.EnsureUUID(pod.GetUID()),
				)
				if err != nil {
					log.Fatal(errors.Wrap(err, fmt.Sprintf("can't delete pod %s from database", pod.GetName())))
				}
			}
		}

		fmt.Fprintln(w, fmt.Sprintf("%d Pods deleted", counter))
	}
}

func startTestCpu(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")

		pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatal(errors.Wrap(err, "can't get pod"))
		}

		_, err = db.Exec(
			"INSERT INTO pod_test (pod_uuid, test) VALUES (?, ?)",
			schemav1.EnsureUUID(pod.GetUID()),
			"cpu",
		)
		if err != nil {
			log.Fatal(errors.Wrap(err, fmt.Sprintf("can't insert pod %s into database", pod.GetName())))
		}

		fmt.Fprintln(w, fmt.Sprintf("CPU test started for pod %s", name))
	}
}

func startTestMemory(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")

		pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatal(errors.Wrap(err, "can't get pod"))
		}

		_, err = db.Exec(
			"INSERT INTO pod_test (pod_uuid, test) VALUES (?, ?)",
			schemav1.EnsureUUID(pod.GetUID()),
			"memory",
		)
		if err != nil {
			log.Fatal(errors.Wrap(err, fmt.Sprintf("can't insert pod %s into database", pod.GetName())))
		}

		fmt.Fprintln(w, fmt.Sprintf("Memory test started for pod %s", name))
	}
}

func stopTestCpu(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")

		pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatal(errors.Wrap(err, "can't get pod"))
		}

		_, err = db.Exec(
			"DELETE FROM pod_test WHERE pod_uuid = ? AND test = ?",
			schemav1.EnsureUUID(pod.GetUID()),
			"cpu",
		)
		if err != nil {
			log.Fatal(errors.Wrap(err, fmt.Sprintf("can't delete pod %s into database", pod.GetName())))
		}

		fmt.Fprintln(w, fmt.Sprintf("CPU test stopped for pod %s", name))
	}
}

func stopTestMemory(clientset *kubernetes.Clientset, namespace string, db *sql.DB) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")

		pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatal(errors.Wrap(err, "can't get pod"))
		}

		_, err = db.Exec(
			"DELETE FROM pod_test WHERE pod_uuid = ? AND test = ?",
			schemav1.EnsureUUID(pod.GetUID()),
			"memory",
		)
		if err != nil {
			log.Fatal(errors.Wrap(err, fmt.Sprintf("can't delete pod %s into database", pod.GetName())))
		}

		fmt.Fprintln(w, fmt.Sprintf("Memory test stopped for pod %s", name))
	}
}
