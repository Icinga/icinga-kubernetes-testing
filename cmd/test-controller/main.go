package main

import (
	_ "github.com/go-sql-driver/mysql"
	"github.com/icinga/icinga-kubernetes-testing/pkg/contracts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"database/sql"
	"flag"
	"github.com/pkg/errors"
	"k8s.io/client-go/util/homedir"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

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
	ctx := signals.SetupSignalHandler()
	logger := klog.FromContext(ctx)

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		logger.Error(err, "Error building kubeconfig")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "Error building kubernetes clientset")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	icingaClient, err := icingav1client.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "Error building kubernetes clientset")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		time.Second*30,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = contracts.TestingLabel
		}),
	)
	icingaInformerFactory := icingainformers.NewSharedInformerFactory(icingaClient, time.Second*30)

	db, err := sql.Open("mysql", "testing:testing@tcp(172.18.0.2)/testing")
	if err != nil {
		klog.Fatal(errors.Wrap(err, "Can't connect to database"))
	}
	defer db.Close()

	c := controller.NewController(
		ctx,
		kubeClient,
		icingaClient,
		kubeInformerFactory.Apps().V1().Deployments(),
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
