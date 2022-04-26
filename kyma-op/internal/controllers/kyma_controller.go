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
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yaml2 "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clustersv1alpha1 "github.com/adityabhatia/kyma-op-poc/kyma-op/api/v1alpha1"
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

	// read config map
	if err := r.ReconcileFromConfigMap(ctx, req); err != nil {
		logger.Error(err, "config map error")
		return ctrl.Result{}, err
	}

	if kymaObj.Spec.Name != kymaObj.Status.State {
		kymaObj.Status.State = kymaObj.Spec.Name
		r.Status().Update(ctx, kymaObj)
		logger.Info("setting", "status", kymaObj.Status.State)
	}

	return ctrl.Result{}, nil
}

func (r *KymaReconciler) ReconcileFromConfigMap(ctx context.Context, req ctrl.Request) error {
	logger := log.FromContext(ctx)
	configMapFound := &corev1.ConfigMap{}
	err := r.Get(context.TODO(), types.NamespacedName{Name: "kyma-component-config", Namespace: req.Namespace}, configMapFound)
	if err != nil && apiErrors.IsNotFound(err) {
		logger.Error(err, "ConfigMap not found… wont deploy untill config map is found\n")
		return err
	}

	serverless, ok := configMapFound.Data["serverless"]
	if !ok {
		return errors.New("serverless component now found")
	}
	istio, ok := configMapFound.Data["istio"]
	if !ok {
		return errors.New("serverless component now found")
	}

	serverlessInfo := make(map[string]interface{})
	if err = yaml.Unmarshal([]byte(serverless), &serverlessInfo); err != nil {
		return errors.New("error reading serverless info file")
	}

	istioInfo := make(map[string]interface{})
	if err = yaml.Unmarshal([]byte(istio), &istioInfo); err != nil {
		return errors.New("error reading istio info file")
	}

	vs := &unstructured.Unstructured{}
	dec := yaml2.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	if _, _, err := dec.Decode([]byte(serverless), nil, vs); err != nil {
		return errors.New("decode error")
	}

	//dynClient := r.GetDynamicClient(ctx)

	//obj := v1alpha1.ServerlessConfiguration{}
	//obj.Name = "recon-name-trial"
	//obj.Namespace = req.Namespace
	//if err := r.Client.Create(ctx, &obj, &client.CreateOptions{}); err != nil {
	//	return err
	//}

	serverlessObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       serverlessInfo["kind"].(string),
			"apiVersion": serverlessInfo["group"].(string) + "/" + serverlessInfo["version"].(string),
			"metadata": map[string]interface{}{
				"name":      "some-name",
				"namespace": req.Namespace,
			},
			"spec": serverlessInfo["spec"],
		},
	}

	//res := dynClient.Resource(schema.GroupVersionResource{
	//	Group:    serverlessInfo["group"].(string),
	//	Resource: serverlessInfo["resource"].(string),
	//	Version:  serverlessInfo["version"].(string),
	//})
	if err := r.Client.Create(context.Background(), serverlessObj, &client.CreateOptions{}); err != nil {
		return errors.Wrap(err, "error creating serverless resource")
	}

	return nil
}

func (r *KymaReconciler) GetDynamicClient(ctx context.Context) dynamic.Interface {
	logger := log.FromContext(ctx)

	//kubeConfig := flag.String("kubeconfig", "/Users/d063994/.kube/config", "location of kubeconfig")
	//config, err := clientcmd.BuildConfigFromFlags("", "/Users/d063994/.kube/config")
	config, err := clientcmd.BuildConfigFromFlags("", "/Users/d063994/SAPDevelop/go/kyma-op-poc/kyma-op/kubeconfig.yaml")
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
	return dynamicClient

	//resources, err := dynamicClient.Resource(schema.GroupVersionResource{
	//	Group:    "kyma.kyma-project.io",
	//	Resource: "serverlessconfigurations",
	//	Version:  "v1alpha1",
	//}).List(ctx, metav1.ListOptions{})
	//if err != nil {
	//	logger.Error(err, "resource could not be fetched")
	//}
	//
	//logger.Info("found", "resource", resources.Items[0].GetName())
	//return dynamicClient
}

// SetupWithManager sets up the controller with the Manager.
func (r *KymaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clustersv1alpha1.Kyma{}).
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{})).
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
