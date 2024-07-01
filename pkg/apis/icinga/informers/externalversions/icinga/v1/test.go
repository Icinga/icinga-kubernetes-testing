// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	versioned "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/clientset/versioned"
	internalinterfaces "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/informers/externalversions/internalinterfaces"
	v1 "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/listers/icinga/v1"
	icingav1 "github.com/icinga/icinga-kubernetes-testing/pkg/apis/icinga/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// TestInformer provides access to a shared informer and lister for
// Tests.
type TestInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.TestLister
}

type testInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewTestInformer constructs a new informer for Test type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewTestInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredTestInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredTestInformer constructs a new informer for Test type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredTestInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IcingaV1().Tests(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IcingaV1().Tests(namespace).Watch(context.TODO(), options)
			},
		},
		&icingav1.Test{},
		resyncPeriod,
		indexers,
	)
}

func (f *testInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredTestInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *testInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&icingav1.Test{}, f.defaultInformer)
}

func (f *testInformer) Lister() v1.TestLister {
	return v1.NewTestLister(f.Informer().GetIndexer())
}
