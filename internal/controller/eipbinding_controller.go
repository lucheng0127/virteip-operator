/*
Copyright 2024.

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
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	virteipv1 "github.com/lucheng0127/virteip-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	vmv1 "kubevirt.io/api/core/v1"
)

// EipBindingReconciler reconciles a EipBinding object
type EipBindingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Get hyper node ipv4 address
func (r *EipBindingReconciler) getHyperIPAddr(name string) (string, error) {
	node := &corev1.Node{}
	err := r.Client.Get(context.TODO(), client.ObjectKey{
		Name: name,
	}, node)

	if err != nil {
		return "", err
	}

	return node.Status.Addresses[0].Address, nil
}

// Get hyper node ipv4 address that kubevirt vmi located on and vmi ipv4 address
func (r *EipBindingReconciler) getVmiInfo(eb virteipv1.EipBinding) (string, string, error) {
	var vmi vmv1.VirtualMachineInstance
	err := r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      eb.Spec.Vmi,
		Namespace: eb.Namespace,
	}, &vmi)
	if err != nil {
		return "", "", err
	}

	if vmi.Status.Phase != "Running" {
		return "", "", errors.NewNotFound(vmv1.Resource("VirtualMachineInstance"), "VirtualMachineInstance")
	}

	hyperAddr, err := r.getHyperIPAddr(vmi.Status.NodeName)
	if err != nil {
		return "", "", err
	}

	if len(vmi.Status.Interfaces) == 0 {
		return "", "", errors.NewBadRequest("vmi without interface, can't bind")
	}

	return hyperAddr, vmi.Status.Interfaces[0].IP, nil
}

//+kubebuilder:rbac:groups=virteip.github.com,resources=eipbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=virteip.github.com,resources=eipbindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=virteip.github.com,resources=eipbindings/finalizers,verbs=update
//+kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachineinstances,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EipBinding object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.0/pkg/reconcile
func (r *EipBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Get current eipbinding

	// Get current hyper and vmi ip info

	// Compare last and current hyper and vmi info
	// if vmi ip change, sync eipbing rules via job
	// if hyper change, vmi ip not change skip

	// Generate job to sync eipbinding rules

	// Clean jobs according to job history limit

	// Update last hyper and vmi ip info

	return ctrl.Result{}, nil
}

func (r *EipBindingReconciler) findObjectsForVmi(ctx context.Context, vmi client.Object) []reconcile.Request {
	attachedEipBinding := &virteipv1.EipBindingList{}
	listOps := client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(".spec.vmiName", vmi.GetName()),
		Namespace:     vmi.GetNamespace(),
	}

	err := r.List(context.TODO(), attachedEipBinding, &listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(attachedEipBinding.Items))
	for i, item := range attachedEipBinding.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}

	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *EipBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&virteipv1.EipBinding{}).
		Owns(&vmv1.VirtualMachineInstance{}).
		Watches(
			&vmv1.VirtualMachineInstance{},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForVmi),
		).
		Complete(r)
}
