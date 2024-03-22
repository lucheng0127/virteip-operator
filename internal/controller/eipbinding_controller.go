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
	"math/rand"
	"strings"
	"time"

	kbatch "k8s.io/api/batch/v1"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vmv1 "kubevirt.io/api/core/v1"
)

const (
	JobImg       = "quay.io/shawnlu0127/eipctl:20240319"
	ActionBind   = "bind"
	ActionUnbind = "unbind"
	JoinKey      = ".metadata.controller"
)

var eipctlCmd = "/eipctl --target %s:6127 --eip-ip %s --vmi-ip %s --action %s"

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

	vmiip := vmi.Status.Interfaces[0].IP
	if hyperAddr == "" || vmiip == "" {
		// Vmi info not sync finished
		return "", "", nil
	}

	return hyperAddr, vmiip, nil
}

func randStrings(n int) string {
	letterRunes := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

// Construct job for sync eip rules
func (r *EipBindingReconciler) constructEipBindingJob(eb virteipv1.EipBinding, cmd string) (*kbatch.Job, error) {
	name := fmt.Sprintf("eip-%s-binding-%s", eb.Spec.EipAddr, randStrings(8))
	name = strings.Replace(name, ".", "-", -1)
	backoflimit := int32(0)

	job := &kbatch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   eb.Namespace,
		},
		Spec: kbatch.JobSpec{
			BackoffLimit: &backoflimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    name,
							Image:   JobImg,
							Command: strings.Split(cmd, " "),
						},
					},
				},
			},
		},
	}

	// Set controller reference
	if err := ctrl.SetControllerReference(&eb, job, r.Scheme); err != nil {
		return nil, err
	}

	return job, nil
}

func (r *EipBindingReconciler) syncEipBinding(ctx context.Context, eb virteipv1.EipBinding, action, hyper, eip, vmip string, wait bool) error {
	var cmd string
	if strings.ToLower(action) == ActionBind {
		cmd = fmt.Sprintf(eipctlCmd, hyper, eip, vmip, ActionBind)
	} else {
		cmd = fmt.Sprintf(eipctlCmd, hyper, eip, vmip, ActionUnbind)
	}

	job, err := r.constructEipBindingJob(eb, cmd)
	if err != nil {
		return err
	}

	err = r.Create(ctx, job)
	if !wait || err != nil {
		return err
	}

	// Waiting for job finished
	for {
		var rjob kbatch.Job
		if err := r.Get(ctx, client.ObjectKeyFromObject(job), &rjob); err != nil {
			return err
		}

		if rjob.Status.Succeeded > 0 {
			break
		}

		if rjob.Status.Failed > 0 {
			return fmt.Errorf("clean up job %s in namespace %s failed", &rjob.Name, &rjob.Namespace)
		}

		time.Sleep(time.Second)
	}
	return nil
}

func (r *EipBindingReconciler) deleteStaledJobs(ctx context.Context, eb virteipv1.EipBinding) error {
	var childJobs kbatch.JobList

	if err := r.List(ctx, &childJobs, client.InNamespace(eb.Namespace), client.MatchingFields{JoinKey: eb.Name}); client.IgnoreAlreadyExists(err) != nil {
		return err
	}

	for i, job := range childJobs.Items {
		if int32(i) >= int32(len(childJobs.Items))-*eb.Spec.JobHistory {
			break
		}

		// Set propagationPolicy=Background, nor when job delete, pod will not be delete
		err := r.Delete(ctx, &job, client.PropagationPolicy(metav1.DeletePropagationBackground))
		if err != nil {
			return err
		}
	}

	return nil
}

func ignoreErrs(err error) error {
	if err == nil {
		return nil
	}

	if strings.Contains(err.Error(), "resource name may not be empty") {
		return nil
	}

	return err
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

	if err := r.Get(ctx, req.NamespacedName, &eb); client.IgnoreNotFound(err) != nil {
		log.Error(err, "unable to fetch EipBinding")
		return ctrl.Result{}, err
	}

	// Update last hyper and vmi ip info before exist if vmi hyper or ip info changed
	needUpdate := false
	defer func() {
		if needUpdate {
			if err := r.deleteStaledJobs(ctx, eb); client.IgnoreNotFound(err) != nil {
				log.Error(err, "delete staled jobs")
			}

			if err := r.Update(ctx, &eb); ignoreErrs(err) != nil {
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
			// Do clean up
			log.Info(fmt.Sprintf("Clean up staled eip rules [1] eip_%s<->vmip_%s on %s", eb.Spec.EipAddr, eb.Spec.LastIP, eb.Spec.LastHyper))
			err := r.syncEipBinding(ctx, eb, ActionUnbind, eb.Spec.LastHyper, eb.Spec.EipAddr, eb.Spec.LastIP, true)
			if err != nil {
				log.Error(err, "clean up eip rules [1]")
				// Do not return nor will block crd delete
				//return ctrl.Result{}, err
			}
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
	staledHyper := eb.Spec.LastHyper
	staledIP := eb.Spec.LastIP
	eb.Spec.LastHyper = currentHyper
	eb.Spec.LastIP = currentIP
	needUpdate = true

	if staledHyper != "" && staledIP != "" {
		// up staled eip rules
		log.Info(fmt.Sprintf("Clean up staled eip rules [2] eip_%s<->vmip_%s on %s", eb.Spec.EipAddr, staledIP, staledHyper))
		err := r.syncEipBinding(ctx, eb, ActionUnbind, staledHyper, eb.Spec.EipAddr, staledIP, false)

		if err != nil {
			log.Error(err, "clean up eip rules [2]")
			return ctrl.Result{}, err
		}
	}

	// Apply eip rules to current hyper and current ipv4 address
	if currentHyper != "" && currentIP != "" {
		log.Info(fmt.Sprintf("Apply hyper eip rules [1] eip_%s<->vmip_%s on %s", eb.Spec.EipAddr, currentIP, currentHyper))
		err = r.syncEipBinding(ctx, eb, ActionBind, currentHyper, eb.Spec.EipAddr, currentIP, false)

		if err != nil {
			log.Error(err, "apply eip rules [1]")
			return ctrl.Result{}, err
		}
	}

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

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &kbatch.Job{}, JoinKey, func(rawObj client.Object) []string {
		job := rawObj.(*kbatch.Job)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != virteipv1.GroupVersion.String() {
			return nil
		}

		return []string{owner.Name}
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
