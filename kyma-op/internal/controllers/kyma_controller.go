/*
Copyright 2022.

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

package controllers

import (
	"context"
	"fmt"
	clustersv1alpha1 "github.com/adityabhatia/kyma-op-poc/kyma-op/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KymaReconciler reconciles a Kyma object
type KymaReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=clusters.kyma-project.io,resources=kymas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clusters.kyma-project.io,resources=kymas/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=clusters.kyma-project.io,resources=kymas/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Kyma object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *KymaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile Starting for", "request", req)
	// TODO(user): your logic here

	kymaObj := &clustersv1alpha1.Kyma{}
	r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, kymaObj)

	configMap, err := r.GetConfigMap(ctx, req)
	if err != nil {
		logger.Error(err, "config map not found")
	}

	controllerutil.SetOwnerReference(kymaObj, configMap, r.Scheme)
	r.Update(ctx, configMap)

	// read config map
	if err := r.ReconcileFromConfigMap(ctx, req, configMap, kymaObj.Spec.Components); err != nil {
		logger.Error(err, "component CR creation error")
		return ctrl.Result{}, err
	}

	// perform some operation on KymaCR
	if kymaObj.Status.State != "INITIAL_STATE" {
		kymaObj.Status.State = "PENDING_STATE"
		r.Status().Update(ctx, kymaObj)
		logger.Info("setting", "status", kymaObj.Status.State)
	}

	return ctrl.Result{}, nil
}

func (r *KymaReconciler) GetConfigMap(ctx context.Context, req ctrl.Request) (*corev1.ConfigMap, error) {
	logger := log.FromContext(ctx)
	configMapFound := &corev1.ConfigMap{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: "kyma-component-config", Namespace: req.Namespace}, configMapFound)
	if err != nil && apiErrors.IsNotFound(err) {
		logger.Error(err, "ConfigMap not found… wont deploy untill config map is found\n")
		return nil, err
	}
	return configMapFound, nil
}

func (r *KymaReconciler) ReconcileFromConfigMap(ctx context.Context, req ctrl.Request, configMap *corev1.ConfigMap, kymaComponents []clustersv1alpha1.ComponentType) error {
	logger := log.FromContext(ctx)
	for _, component := range kymaComponents {
		componentName := component.Name + "-name"

		componentBytes, ok := configMap.Data[component.Name]
		if !ok {
			return fmt.Errorf("%s serverless component now found", component.Name)
		}

		componentYaml := make(map[string]interface{})
		if err := yaml.Unmarshal([]byte(componentBytes), &componentYaml); err != nil {
			return fmt.Errorf("error during config map unmarshal %w", err)
		}

		gvr := schema.GroupVersionResource{
			Group:    componentYaml["group"].(string),
			Resource: componentYaml["resource"].(string),
			Version:  componentYaml["version"].(string),
		}

		if r.GetResource(ctx, gvr, componentName, req.Namespace) {
			continue
		}

		componentUnstructured := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind":       componentYaml["kind"].(string),
				"apiVersion": componentYaml["group"].(string) + "/" + componentYaml["version"].(string),
				"metadata": map[string]interface{}{
					"name":      componentName,
					"namespace": req.Namespace,
				},
				"spec": componentYaml["spec"],
			},
		}

		if err := r.Client.Create(context.Background(), componentUnstructured, &client.CreateOptions{}); err != nil {
			return fmt.Errorf("error creating custom resource of type %s %w", component.Name, err)
		}

		logger.Info("successfully created component CR of", "type", component.Name)
	}

	// TODO: check why a gvk generic DynamicClient doesn't perform create operations
	//dynClient := r.GetDynamicClient(ctx)
	//res := dynClient.Resource(schema.GroupVersionResource{
	//	Group:    serverlessInfo["group"].(string),
	//	Resource: serverlessInfo["resource"].(string),
	//	Version:  serverlessInfo["version"].(string),
	//})

	return nil
}

func (r *KymaReconciler) GetResource(ctx context.Context, gvr schema.GroupVersionResource, name string, namespace string) bool {
	logger := log.FromContext(ctx)

	//kubeConfig := flag.String("kubeconfig", "/Users/d063994/.kube/config", "location of kubeconfig")
	config, err := clientcmd.BuildConfigFromFlags("", "/Users/d063994/.kube/config")
	if err != nil {
		// check in cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			logger.Error(err, "cluster config could not read")
		}
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		logger.Error(err, "error while creating dynamic client")
	}

	resource, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		logger.Error(err, "resource could not be fetched")
		return false
	}

	logger.Info("resource already exists", "resource_type", resource.GetName())
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *KymaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clustersv1alpha1.Kyma{}).
		Owns(&corev1.ConfigMap{}).
		//Watches(
		//	&source.Kind{Type: &corev1.ConfigMap{}},
		//	handler.EnqueueRequestsFromMapFunc(r.findObjectsForConfigMap),
		//	builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		//).
		Complete(r)
}

func (r *KymaReconciler) findObjectsForConfigMap(configMap client.Object) []reconcile.Request {
	requests := make([]reconcile.Request, 0)
	return requests
}
