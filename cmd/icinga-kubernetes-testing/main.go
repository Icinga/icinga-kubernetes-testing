package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"runtime"
	"time"
)

var isReady bool = true
var isLive bool = true

func main() {
	http.HandleFunc("/cpu", simulateCPUProblem)
	http.HandleFunc("/memory", simulateMemoryProblem)
	http.HandleFunc("/disk", simulateDiskProblem)
	http.HandleFunc("/network", simulateNetworkProblem)
	http.HandleFunc("/oom", simulateOOMProblem)
	http.HandleFunc("/crash", simulateCrashProblem)
	http.HandleFunc("/stopReadiness", simulateReadinessProblem)
	http.HandleFunc("/stopLiveness", simulateLivenessProblem)

	http.HandleFunc("/readiness", readinessProbe)
	http.HandleFunc("/liveness", livenessProbe)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err.Error())
	}
}

func simulateCPUProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating CPU load")
	numCPU := runtime.NumCPU()
	for i := 0; i < numCPU; i++ {
		go func() {
			for {
				_ = math.Sin(math.Pi)
				time.Sleep(200 * time.Nanosecond)
			}
		}()
	}
	fmt.Fprintln(w, "CPU load simulation started on all cores")
}

func simulateMemoryProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating memory load")
	go func() {
		mem := make([]byte, 0)
		for {
			mem = append(mem, make([]byte, 200*1024*1024)...) // Allocate 100MB
			time.Sleep(500 * time.Millisecond)
		}
	}()
	fmt.Fprintln(w, "Memory load simulation started")
}

func simulateDiskProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating disk usage")
	go func() {
		f, err := os.Create("/tmp/bigfile")
		if err != nil {
			log.Println("Error creating file:", err)
			return
		}
		defer f.Close()
		for {
			_, err := f.Write(make([]byte, 1024*1024)) // Write 1MB
			if err != nil {
				log.Println("Error writing to file:", err)
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()
	fmt.Fprintln(w, "Disk usage simulation started")
}

func simulateNetworkProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating network issues")
	// Implementation for network issues simulation
	fmt.Fprintln(w, "Network issues simulation started")
}

func simulateOOMProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating OOM")
	go func() {
		mem := make([]byte, 0)
		for {
			mem = append(mem, make([]byte, 100*1024*1024)...) // Allocate 100MB
			time.Sleep(1 * time.Second)
		}
	}()
	fmt.Fprintln(w, "OOM simulation started")
}

func simulateCrashProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating crash")
	go func() {
		time.Sleep(5 * time.Second)
		os.Exit(1)
	}()
	fmt.Fprintln(w, "Crash simulation started")
}

func simulateReadinessProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating readiness probe failure")
	isReady = false
	fmt.Fprintln(w, "Readiness probe failure simulation started")
}

func simulateLivenessProblem(w http.ResponseWriter, r *http.Request) {
	log.Println("Simulating liveness probe failure")
	isLive = false
	fmt.Fprintln(w, "Liveness probe failure simulation started")
}

func readinessProbe(w http.ResponseWriter, r *http.Request) {
	if isReady {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Ready")
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, "Not Ready")
	}
}

func livenessProbe(w http.ResponseWriter, r *http.Request) {
	if isLive {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Alive")
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, "Not Alive")
	}
}
