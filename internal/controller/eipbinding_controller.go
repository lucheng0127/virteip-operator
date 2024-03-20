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
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	log := log.FromContext(ctx)

	// Get current eipbinding
	var eb virteipv1.EipBinding

	if err := r.Get(ctx, req.NamespacedName, &eb); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		log.Error(err, "unable to fetch EipBinding")
		return ctrl.Result{}, err
	}

	// Update last hyper and vmi ip info before exist if vmi hyper or ip info changed
	needUpdate := false
	defer func() {
		if needUpdate {
			if err := r.Update(ctx, &eb); err != nil {
				log.Error(err, "update Eipbinding")
			}
		}
	}()

	// Handle delete EipBinding CRD
	finalizerName := "virteip.github.com/finlizer"
	if eb.ObjectMeta.DeletionTimestamp.IsZero() {
		// EipBinding is not been deleted, register finalizer
		if !controllerutil.ContainsFinalizer(&eb, finalizerName) {
			controllerutil.AddFinalizer(&eb, finalizerName)
			needUpdate = true
			return ctrl.Result{}, nil
		}
	} else {
		// EipBinding is being deleted
		if controllerutil.ContainsFinalizer(&eb, finalizerName) {
			// TODO(shawnlu): Do clean up

			log.Info(fmt.Sprintf("Clean up staled eip rules eip_%s<->vmip_%s on %s", eb.Spec.EipAddr, eb.Spec.LastIP, eb.Spec.LastHyper))
		}

		controllerutil.RemoveFinalizer(&eb, finalizerName)
		needUpdate = true
		log.Info("Delete EipBinding finished")
		return ctrl.Result{}, nil
	}

	// Get current hyper and vmi ip info
	currentHyper, currentIP, err := r.getVmiInfo(eb)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "unable to get vmi")
			return ctrl.Result{}, err
		}

		//log.Info("VMI offline")
	}

	// Compare last and current hyper and vmi info
	// if vmi ip change, sync eipbing rules via job
	// if hyper change, vmi ip not change skip
	if eb.Spec.LastIP == currentIP {
		// For vmi, if hyper change, vmi ip must change, so if vmi ip not change
		// it means vmi is migrating and the vmi info not sync finished
		//log.Info("Vmi info not update finished, skip")
		return ctrl.Result{}, nil
	}

	// Generate job to sync eipbinding rules
	staleHyper := eb.Spec.LastHyper
	staleIP := eb.Spec.LastIP
	eb.Spec.LastHyper = currentHyper
	eb.Spec.LastIP = currentIP
	needUpdate = true

	if staleHyper != "" && staleIP != "" {
		// TODO(shawnlu): Clean up staled eip rules
		log.Info(fmt.Sprintf("Clean up staled eip rules eip_%s<->vmip_%s on %s", eb.Spec.EipAddr, staleIP, staleHyper))
	}

	// TODO(shawnlu): apply eip rules to current hyper and current ipv4 address
	if currentHyper == "" {
		return ctrl.Result{}, nil
	}
	log.Info(fmt.Sprintf("Apply hyper eip rules eip_%s<->vmip_%s on %s", eb.Spec.EipAddr, currentIP, currentHyper))

	// Clean jobs according to job history limit

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
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &virteipv1.EipBinding{}, ".spec.vmiName", func(rawObj client.Object) []string {
		eb := rawObj.(*virteipv1.EipBinding)
		if eb.Spec.Vmi == "" {
			return nil
		}
		return []string{eb.Spec.Vmi}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&virteipv1.EipBinding{}).
		Owns(&vmv1.VirtualMachineInstance{}).
		Watches(
			&vmv1.VirtualMachineInstance{},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForVmi),
		).
		Complete(r)
}
