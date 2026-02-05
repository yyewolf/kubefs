package kubefs

import (
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
)

// A map to keep track of active informers for all resources
var activeInformers = make(map[schema.GroupVersionResource]cache.SharedInformer)

// Stop channel for all informers
var stopCh chan struct{}

func Inform(kubefs *KubeFS) {
	// Load Kubernetes configuration
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = clientcmd.RecommendedHomeFile
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			klog.Fatalf("Error building kubeconfig: %v", err)
		}
	}

	// Create a Kubernetes clientset for standard resources (used for CRD informer)
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating kubernetes clientset: %v", err)
	}

	// Load namespaces and watch for namespace changes
	namespaceInformerFactory := informers.NewSharedInformerFactory(kubeClient, time.Second*30)
	namespaceInformer := namespaceInformerFactory.Core().V1().Namespaces().Informer()

	namespaceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ns := obj.(*corev1.Namespace)
			klog.Infof("Namespace Added: %s", ns.Name)
			kubefs.EnsureNamespace(ns.Name, false)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldNs := oldObj.(*corev1.Namespace)
			newNs := newObj.(*corev1.Namespace)
			if oldNs.ResourceVersion != newNs.ResourceVersion {
				klog.Infof("Namespace Updated: %s (resourceVersion: %s -> %s)", newNs.Name, oldNs.ResourceVersion, newNs.ResourceVersion)
				// For simplicity, we treat updates as no-ops for now. In production, you might want to handle status changes or labels that affect visibility.
			}
		},
		DeleteFunc: func(obj interface{}) {
			ns, ok := obj.(*corev1.Namespace)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					klog.Errorf("error decoding object, invalid type")
					return
				}
				ns, ok = tombstone.Obj.(*corev1.Namespace)
				if !ok {
					klog.Errorf("error decoding object tombstone, invalid type")
					return
				}
			}
			klog.Infof("Namespace Deleted: %s", ns.Name)
			kubefs.DeleteNamespace(ns.Name)
		},
	})

	// Create an apiextensions clientset for CRDs
	apiextensionsClient, err := apiextensionsclientset.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating apiextensions clientset: %v", err)
	}

	// Create a dynamic client for custom resources
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating dynamic client: %v", err)
	}

	kubefs.client = dynamicClient

	// Create a shared informer factory for apiextensions (specifically for CRDs)
	apiextensionsInformerFactory := apiextensionsinformers.NewSharedInformerFactory(apiextensionsClient, time.Second*30)
	crdInformer := apiextensionsInformerFactory.Apiextensions().V1().CustomResourceDefinitions().Informer()

	// Register event handlers for CRD additions and deletions
	crdInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			crd := obj.(*apiextensionsv1.CustomResourceDefinition)
			klog.Infof("CRD Added: %s", crd.Name)
			addCRDInformer(dynamicClient, crd, kubefs)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldCrd := oldObj.(*apiextensionsv1.CustomResourceDefinition)
			newCrd := newObj.(*apiextensionsv1.CustomResourceDefinition)
			// Only re-add if spec changes, which might affect GVRs or validation
			if oldCrd.ResourceVersion != newCrd.ResourceVersion {
				klog.Infof("CRD Updated: %s (resourceVersion: %s -> %s)", newCrd.Name, oldCrd.ResourceVersion, newCrd.ResourceVersion)
				// For simplicity, we stop the old and start a new. In production, careful diffing is needed.
				removeCRDInformer(oldCrd)
				addCRDInformer(dynamicClient, newCrd, kubefs)
			}
		},
		DeleteFunc: func(obj interface{}) {
			crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					klog.Errorf("error decoding object, invalid type")
					return
				}
				crd, ok = tombstone.Obj.(*apiextensionsv1.CustomResourceDefinition)
				if !ok {
					klog.Errorf("error decoding object tombstone, invalid type")
					return
				}
			}
			klog.Infof("CRD Deleted: %s", crd.Name)
			removeCRDInformer(crd)
		},
	})

	klog.Info("Starting CRD informer...")
	apiextensionsInformerFactory.Start(stopCh)
	namespaceInformerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, crdInformer.HasSynced, namespaceInformer.HasSynced) {
		klog.Fatalf("Failed to sync CRD informer cache")
	}
	klog.Info("Informers synced. Discovering server resources...")

	// Discover all server resources (native + CRDs)
	discoverResources(kubeClient, dynamicClient, kubefs)
}

func addCRDInformer(dynamicClient dynamic.Interface, crd *apiextensionsv1.CustomResourceDefinition, kubefs *KubeFS) {
	// CRDs can define multiple versions, we need to pick one or handle all.
	// For simplicity, we'll iterate through all versions and create an informer for each.
	// In a production scenario, you might only care about the storage version or latest stable version.
	for _, version := range crd.Spec.Versions {
		if !version.Storage || !version.Served {
			continue // Only add informers for served versions that are marked as storage (or you could choose differently based on your needs)
		}

		gvr := schema.GroupVersionResource{
			Group:    crd.Spec.Group,
			Version:  version.Name,
			Resource: crd.Spec.Names.Plural,
		}

		addInformer(dynamicClient, gvr, crd.Spec.Names.Kind, kubefs)
	}
}

func removeCRDInformer(crd *apiextensionsv1.CustomResourceDefinition) {
	for _, version := range crd.Spec.Versions {
		gvr := schema.GroupVersionResource{
			Group:    crd.Spec.Group,
			Version:  version.Name,
			Resource: crd.Spec.Names.Plural,
		}

		if _, exists := activeInformers[gvr]; exists {
			klog.Infof("Stopping and removing dynamic informer for custom resource: %s", gvr.String())
			// This is tricky. There's no direct "Stop" method on cache.SharedInformer
			// or dynamicinformer.DynamicSharedInformerFactory for individual informers.
			// The stopCh passed to factory.Start() stops ALL informers in that factory.
			// For a true dynamic removal, you'd need a factory per GVR and manage their stop channels individually.
			// For this example, we'll mark it as inactive and rely on the main stopCh.
			// A more robust solution might involve canceling the context used to start the individual informer.
			delete(activeInformers, gvr)
			klog.Warningf("Informer for %s marked for removal. Actual goroutine might persist until main stopCh closes.", gvr.String())
		}
	}
}

func addInformer(dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, kind string, kubefs *KubeFS) {
	if _, exists := activeInformers[gvr]; exists {
		return
	}

	klog.Infof("Adding dynamic informer for resource: %s (Kind: %s)", gvr.String(), kind)

	// Create a DynamicSharedInformerFactory for this specific GVR
	dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		dynamicClient,
		time.Minute*5,       // Resync period
		metav1.NamespaceAll, // Watch all namespaces
		nil,                 // TweakListOptionsFunc (optional)
	)

	// Get the informer for the specific GVR
	informer := dynamicInformerFactory.ForResource(gvr).Informer()

	// Add event handlers
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			unstructuredObj := obj.(*unstructured.Unstructured)
			klog.Infof("Resource ADDED [%s]: %s/%s", gvr.String(), unstructuredObj.GetNamespace(), unstructuredObj.GetName())

			kubefs.AddResource(unstructuredObj.GetName(), gvr.Resource, unstructuredObj.GetNamespace(), schema.GroupVersionKind{
				Group:   gvr.Group,
				Version: gvr.Version,
				Kind:    kind,
			})
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldUnstructuredObj := oldObj.(*unstructured.Unstructured)
			newUnstructuredObj := newObj.(*unstructured.Unstructured)
			if oldUnstructuredObj.GetResourceVersion() != newUnstructuredObj.GetResourceVersion() {
				// klog.V(4).Infof("Resource UPDATED [%s]: %s/%s", gvr.String(), newUnstructuredObj.GetNamespace(), newUnstructuredObj.GetName())
			}
		},
		DeleteFunc: func(obj interface{}) {
			unstructuredObj, ok := obj.(*unstructured.Unstructured)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					klog.Errorf("error decoding object, invalid type")
					return
				}
				unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
				if !ok {
					klog.Errorf("error decoding object tombstone, invalid type")
					return
				}
			}
			klog.Infof("Resource DELETED [%s]: %s/%s", gvr.String(), unstructuredObj.GetNamespace(), unstructuredObj.GetName())

			kubefs.DeleteResource(unstructuredObj.GetName(), unstructuredObj.GetNamespace(), schema.GroupVersionKind{
				Group:   gvr.Group,
				Version: gvr.Version,
				Kind:    kind,
			})
		},
	})

	// Start the informer
	go dynamicInformerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {
		klog.Errorf("Failed to sync informer cache for GVR: %s", gvr.String())
		return
	}
	activeInformers[gvr] = informer
}

func discoverResources(kubeClient kubernetes.Interface, dynamicClient dynamic.Interface, kubefs *KubeFS) {
	discoveryClient := kubeClient.Discovery()
	resourceLists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		klog.Errorf("Error discovering resources: %v", err)
	}

	for _, resourceList := range resourceLists {
		groupVersion, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			klog.Errorf("Failed to parse GroupVersion %q: %v", resourceList.GroupVersion, err)
			continue
		}

		for _, resource := range resourceList.APIResources {
			if !supportsListAndWatch(resource.Verbs) {
				continue
			}

			// Skip subresources
			if strings.Contains(resource.Name, "/") {
				continue
			}

			gvr := schema.GroupVersionResource{
				Group:    groupVersion.Group,
				Version:  groupVersion.Version,
				Resource: resource.Name,
			}

			addInformer(dynamicClient, gvr, resource.Kind, kubefs)
		}
	}
}

func supportsListAndWatch(verbs []string) bool {
	hasList := false
	hasWatch := false
	for _, verb := range verbs {
		if verb == "list" {
			hasList = true
		}
		if verb == "watch" {
			hasWatch = true
		}
	}
	return hasList && hasWatch
}
