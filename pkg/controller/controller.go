/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net"
	"slices"
	"strconv"
	"time"

	"golang.org/x/time/rate"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	icingav1 "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/v1"
	"github.com/icinga/icinga-kubernetes-testing/pkg/contracts"
	icingav1client "github.com/icinga/icinga-kubernetes-testing/pkg/generated/clientset/versioned"
	icingascheme "github.com/icinga/icinga-kubernetes-testing/pkg/generated/clientset/versioned/scheme"
	informers "github.com/icinga/icinga-kubernetes-testing/pkg/generated/informers/externalversions/icinga/v1"
	listers "github.com/icinga/icinga-kubernetes-testing/pkg/generated/listers/icinga/v1"
	schemav1 "github.com/icinga/icinga-kubernetes/pkg/schema/v1"
)

const controllerAgentName = "icinga-testing-api"

const (
	// SuccessSynced is used as part of the Event 'reason' when a Test is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a Test fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Deployment already existing
	MessageResourceExists = "Resource %q already exists and is not managed by Test"
	// MessageResourceSynced is the message used for an Event fired when a Test
	// is synced successfully
	MessageResourceSynced = "Test synced successfully"
)

// TestController is the testing-api implementation for Test resources
type TestController struct {
	// clientset is a standard kubernetes clientset
	clientset kubernetes.Interface
	// icingaClientset is a clientset for our own API group
	icingaClientset icingav1client.Interface

	deploymentsLister appslisters.DeploymentLister
	deploymentsSynced cache.InformerSynced

	testsLister listers.TestLister
	testsSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	db *sql.DB
}

// NewController returns a new sample testing-api
func NewController(
	ctx context.Context,
	kubeclientset kubernetes.Interface,
	icingaclientset icingav1client.Interface,
	deploymentInformer appsinformers.DeploymentInformer,
	testInformer informers.TestInformer,
	db *sql.DB,
) *TestController {
	logger := klog.FromContext(ctx)

	// Create event broadcaster
	// Add sample-testing-api types to the default Kubernetes Scheme so Events can be
	// logged for sample-testing-api types.
	utilruntime.Must(icingascheme.AddToScheme(scheme.Scheme))
	logger.Info("Creating event broadcaster")

	eventBroadcaster := record.NewBroadcaster(record.WithContext(ctx))
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(
		&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events(contracts.TestingNamespace)},
	)
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	ratelimiter := workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second),
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(50), 300)},
	)

	controller := &TestController{
		clientset:         kubeclientset,
		icingaClientset:   icingaclientset,
		deploymentsLister: deploymentInformer.Lister(),
		deploymentsSynced: deploymentInformer.Informer().HasSynced,
		testsLister:       testInformer.Lister(),
		testsSynced:       testInformer.Informer().HasSynced,
		workqueue:         workqueue.NewRateLimitingQueue(ratelimiter),
		recorder:          recorder,
		db:                db,
	}

	logger.Info("Setting up event handlers")
	// Set up an event handler for when Test resources change
	testInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			test := obj.(*icingav1.Test)
			_, err := db.Exec(
				"INSERT INTO test (uuid, name, namespace, uid, deployment_name, created) VALUES (?, ?, ?, ?, ?, ?)",
				schemav1.EnsureUUID(test.UID),
				test.Name,
				test.Namespace,
				test.UID,
				test.Spec.DeploymentName,
				test.ObjectMeta.CreationTimestamp.UnixMilli(),
			)
			if err != nil {
				logger.Error(err, "Error inserting test into database")
			}
			controller.enqueueTest(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueTest(new)
		},
		DeleteFunc: func(obj interface{}) {
			test := obj.(*icingav1.Test)
			_, err := db.Exec("DELETE FROM test WHERE uuid = ?", schemav1.EnsureUUID(test.UID))
			if err != nil {
				logger.Error(err, "Error deleting test from database")
			}
		},
	})
	// Set up an event handler for when Deployment resources change. This
	// handler will lookup the owner of the given Deployment, and if it is
	// owned by a Test resource then the handler will enqueue that Test resource for
	// processing. This way, we don't need to implement custom logic for
	// handling Deployment resources. More info on this pattern:
	// https://github.com/kubernetes/community/blob/8cafef897a22026d42f5e5bb3f104febe7e29830/contributors/devel/controllers.md
	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject(ctx),
		UpdateFunc: func(old, new interface{}) {
			newDepl := new.(*appsv1.Deployment)
			oldDepl := old.(*appsv1.Deployment)

			klog.Info("Could handle tests")
			if *newDepl.Spec.Replicas == newDepl.Status.AvailableReplicas {
				controller.handleTests(ctx, newDepl)
			}

			if newDepl.ResourceVersion == oldDepl.ResourceVersion {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			controller.handleObject(ctx)(new)
		},
		DeleteFunc: controller.handleObject(ctx),
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *TestController) Run(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	logger := klog.FromContext(ctx)

	// Start the informer factories to begin populating the informer caches
	logger.Info("Starting Test testing-api")

	// Wait for the caches to be synced before starting workers
	logger.Info("Waiting for informer caches to sync")

	if ok := cache.WaitForCacheSync(ctx.Done(), c.deploymentsSynced, c.testsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	logger.Info("Starting workers", "count", workers)
	// Launch two workers to process Test resources
	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	logger.Info("Started workers")
	<-ctx.Done()
	logger.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *TestController) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *TestController) processNextWorkItem(ctx context.Context) bool {
	obj, shutdown := c.workqueue.Get()
	logger := klog.FromContext(ctx)

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Test resource to be synced.
		if err := c.syncHandler(ctx, key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		logger.Info("Successfully synced", "resourceName", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Test resource
// with the current status of the resource.
func (c *TestController) syncHandler(ctx context.Context, key string) error {
	logger := klog.FromContext(ctx)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the Test resource with this namespace/name
	test, err := c.testsLister.Tests(namespace).Get(name)
	if err != nil {
		// The Test resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("test '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	deploymentName := test.Spec.DeploymentName
	if deploymentName == "" {
		// We choose to absorb the error here as the worker would requeue the
		// resource otherwise. Instead, the next time the resource is updated
		// the resource will be queued again.
		utilruntime.HandleError(fmt.Errorf("%s: deployment name must be specified", key))
		return nil
	}

	var availableReplicas int32

	for _, t := range test.Spec.Tests {

		// Get the deployment with the name specified in Test.spec
		deployment, err := c.deploymentsLister.Deployments(test.Namespace).Get(deploymentName + "-" + t.TestKind)

		// If the resource doesn't exist, we'll create it
		if errors.IsNotFound(err) {
			deployment, err = c.clientset.AppsV1().Deployments(test.Namespace).Create(
				ctx,
				newDeployment(test, t.TotalReplicas, t.BadReplicas, t.TestKind),
				metav1.CreateOptions{},
			)
		}
		// If an error occurs during Get/Create, we'll requeue the item so we can
		// attempt processing again later. This could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}

		// If the Deployment is not controlled by this Test resource, we should log
		// a warning to the event recorder and return error msg.
		if !metav1.IsControlledBy(deployment, test) {
			msg := fmt.Sprintf(MessageResourceExists, deployment.Name)
			c.recorder.Event(test, corev1.EventTypeWarning, ErrResourceExists, msg)
			return fmt.Errorf("%s", msg)
		}

		// If this number of the replicas on the Test resource is specified, and the
		// number does not equal the current desired replicas on the Deployment, we
		// should update the Deployment resource.
		if t.TotalReplicas != nil && *t.TotalReplicas != *deployment.Spec.Replicas {
			logger.Info(
				"Update deployment resource",
				"currentReplicas",
				*t.TotalReplicas,
				"desiredReplicas",
				*deployment.Spec.Replicas)

			deployment, err = c.clientset.AppsV1().Deployments(test.Namespace).Update(
				ctx,
				newDeployment(test, t.TotalReplicas, t.BadReplicas, t.TestKind),
				metav1.UpdateOptions{},
			)
		}

		// If an error occurs during Update, we'll requeue the item so we can
		// attempt processing again later. This could have been caused by a
		// temporary network failure, or any other transient reason.
		if err != nil {
			return err
		}

		availableReplicas += deployment.Status.AvailableReplicas
	}

	// Finally, we update the status block of the Test resource to reflect the
	// current state of the world
	err = c.updateTestStatus(ctx, test, availableReplicas)
	if err != nil {
		return err
	}

	c.recorder.Event(test, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)

	// Insert the test into a database

	return nil
}

func (c *TestController) updateTestStatus(ctx context.Context, test *icingav1.Test, availableReplicas int32) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	testCopy := test.DeepCopy()
	testCopy.Status.AvailableReplicas = availableReplicas
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the Test resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	_, err := c.icingaClientset.IcingaV1().Tests(test.Namespace).UpdateStatus(
		ctx,
		testCopy,
		metav1.UpdateOptions{},
	)
	return err
}

// enqueueTest takes a Test resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Test.
func (c *TestController) enqueueTest(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	c.workqueue.Add(key)
}

// handleObject will take any resource implementing metav1.Object and attempt
// to find the Test resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that Test resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
func (c *TestController) handleObject(ctx context.Context) func(obj interface{}) {
	logger := klog.FromContext(ctx)

	return func(obj interface{}) {
		var object metav1.Object
		var ok bool
		if object, ok = obj.(metav1.Object); !ok {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
				return
			}
			object, ok = tombstone.Obj.(metav1.Object)
			if !ok {
				utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
				return
			}
			logger.Info("Recovered deleted object", "resourceName", object.GetName())
		}
		logger.Info("Processing object", "object", klog.KObj(object))
		if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
			// If this object is not owned by a Test, we should not do anything more
			// with it.
			if ownerRef.Kind != "Test" {
				return
			}

			test, err := c.testsLister.Tests(object.GetNamespace()).Get(ownerRef.Name)
			if err != nil {
				logger.Info(
					"Ignore orphaned object",
					"object",
					klog.KObj(object),
					"test",
					ownerRef.Name,
				)
				return
			}

			c.enqueueTest(test)
			return
		}
	}
}

func (c *TestController) handleTests(ctx context.Context, deployment *appsv1.Deployment) {
	logger := klog.FromContext(ctx)

	logger.Info("Handling tests", "deployment", deployment.Name)

	labelSelector := metav1.FormatLabelSelector(metav1.SetAsLabelSelector(deployment.Spec.Selector.MatchLabels))

	pods, err := c.clientset.CoreV1().Pods(deployment.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		logger.Error(err, "Error listing pods")
		return
	}

	replicas := int(*deployment.Spec.Replicas)
	badReplicas, _ := strconv.Atoi(deployment.Labels["badReplicas"])
	tester := 0

	for _, pod := range pods.Items {
		if pod.Labels["tester"] == "true" {
			klog.Info(fmt.Sprintf("Already tester: %s", pod.Name))
			tester++
		}
	}

	if tester != badReplicas {
		var index int
		var usedIndexes []int

		for i := 0; i < badReplicas-tester; i++ {
			if replicas == 1 {
				index = 0
			} else {
				index = rand.IntnRange(0, len(pods.Items))
				for slices.Contains(usedIndexes, index) || pods.Items[index].Labels["tester"] == "true" {
					index = rand.IntnRange(0, len(pods.Items))
				}
			}

			pod := pods.Items[index]
			socket := fmt.Sprintf("%s:%s", pod.Status.PodIP, "8080")
			var conn net.Conn

			for j := 0; j < 3; j++ {
				conn, err = net.Dial("tcp", socket)
				if err == nil {
					break
				}

				logger.Error(err, "Error connecting to pod via tcp", "pod", pod.Name, "socket", socket, "attempt", j+1)
				time.Sleep(5 * time.Second)
			}

			if err != nil {
				logger.Error(err, "Failed to connect to pod after 3 attempts", "pod", pod.Name, "socket", socket)
				return
			}
			defer conn.Close()

			writer := bufio.NewWriter(conn)
			_, err = writer.WriteString(fmt.Sprintf("test: %s\n", deployment.Labels["testKind"]))
			if err != nil {
				logger.Error(err, "Error writing to pod", "pod", pod.Name, "socket", socket)
				return
			}
			err = writer.Flush()
			if err != nil {
				logger.Error(err, "Error flushing writer", "pod", pod.Name, "socket", socket)
				return
			}

			_, err = c.clientset.CoreV1().Pods(deployment.Namespace).Patch(
				ctx,
				pod.Name,
				types.JSONPatchType,
				[]byte(fmt.Sprintf(`[{"op": "add", "path": "/metadata/labels/tester", "value": "true"}]`)),
				metav1.PatchOptions{},
			)
			if err != nil {
				logger.Error(err, "Error patching pod", "pod", pod.Name)
				return
			}

			usedIndexes = append(usedIndexes, index)
		}
	}

	//for _, pod := range pods.Items {
	//	logger.Info("Pod of Deployment", "deployment", deployment.Name, "pod", pod.Name, "pod ip", pod.Status.PodIP)
	//}
}

// newDeployment creates a new Deployment for a Test resource. It also sets
// the appropriate OwnerReferences on the resource so handleObject can discover the Test
// resource that 'owns' it. Additionally, it mounts a ConfigMap to the container.
func newDeployment(test *icingav1.Test, replicas *int32, badReplicas *int32, testKind string) *appsv1.Deployment {
	deploymentName := test.Spec.DeploymentName + "-" + testKind
	labels := map[string]string{
		contracts.TestingLabel: "true",
		"deploymentName":       deploymentName,
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: test.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(test, icingav1.SchemeGroupVersion.WithKind("Test")),
			},
			Labels: map[string]string{
				contracts.TestingLabel: "true",
				"badReplicas":          strconv.Itoa(int(*badReplicas)),
				"testKind":             testKind,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "tester",
							Image:           "ikt-tester",
							ImagePullPolicy: "Never",
						},
					},
				},
			},
		},
	}
}
