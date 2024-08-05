package main

import (
	_ "github.com/go-sql-driver/mysql"

	"context"
	"database/sql"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pkg/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"

	"github.com/icinga/icinga-kubernetes-testing/pkg/contracts"
	"github.com/icinga/icinga-kubernetes-testing/pkg/controller"
	icingav1client "github.com/icinga/icinga-kubernetes-testing/pkg/generated/clientset/versioned"
	icingainformers "github.com/icinga/icinga-kubernetes-testing/pkg/generated/informers/externalversions"
)

var (
	masterURL  string
	kubeconfig string
)

func init() {
	flag.StringVar(
		&kubeconfig,
		"kubeconfig",
		homedir.HomeDir()+"/.kube/config",
		"Path to a kubeconfig. Only required if out-of-cluster.",
	)
	flag.StringVar(
		&masterURL,
		"master",
		"",
		"The address of the Kubernetes API server. "+
			"Overrides any value in kubeconfig. Only required if out-of-cluster.",
	)
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// set up signals so we handle the shutdown signal gracefully
	var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

	signals := make(chan os.Signal, 2)
	ctx, cancel := context.WithCancel(context.Background())
	signal.Notify(signals, shutdownSignals...)
	go func() {
		<-signals
		cancel()
		<-signals
		os.Exit(1) // second signal. Exit directly.
	}()
	logger := klog.FromContext(ctx)

	clientset, err := getClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't get Kubernetes clientset"))
	}

	icingaClientset, err := getIcingaClientset()
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't get Icinga clientset"))
	}

	kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		time.Second*30,
		informers.WithNamespace(contracts.TestingNamespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = contracts.TestingLabel
		}),
	)
	icingaInformerFactory := icingainformers.NewSharedInformerFactoryWithOptions(
		icingaClientset,
		time.Second*30,
		icingainformers.WithNamespace(contracts.TestingNamespace),
	)

	db, err := sql.Open("mysql", "testing:testing@tcp(192.168.49.2:30003)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't connect to database"))
	}
	defer func() { _ = db.Close() }()

	c := controller.NewController(
		ctx,
		clientset,
		icingaClientset,
		kubeInformerFactory.Apps().V1().Deployments(),
		kubeInformerFactory.Apps().V1().ReplicaSets(),
		kubeInformerFactory.Apps().V1().StatefulSets(),
		kubeInformerFactory.Apps().V1().DaemonSets(),
		icingaInformerFactory.Icinga().V1().Tests(),
		db,
	)

	kubeInformerFactory.Start(ctx.Done())
	icingaInformerFactory.Start(ctx.Done())

	if err = c.Run(ctx, 2); err != nil {
		logger.Error(err, "Error running testing-api")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
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
