/*
Copyright 2025.

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
	"os"
	"strings"
	"time"

	platformv1alpha1 "github.com/shieldx-bot/shieldx-platform/api/v1alpha1"
	"github.com/shieldx-bot/shieldx-platform/internal/webhook/notify"
	"github.com/shieldx-bot/shieldx-platform/internal/webhook/verifyimage"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// TenantReconciler reconciles a Tenant object
type TenantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=platform.shieldx.io,resources=tenants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.shieldx.io,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.shieldx.io,resources=tenants/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Tenant object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *TenantReconciler) ensureNamespace(
	ctx context.Context,
	tenant *platformv1alpha1.Tenant,
) error {

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-" + tenant.Name,
		},
	}

	_, err := controllerutil.CreateOrUpdate(
		ctx,
		r.Client,
		ns,
		func() error {
			if ns.Labels == nil {
				ns.Labels = map[string]string{}
			}
			ns.Labels["tenant"] = tenant.Name

			return ctrl.SetControllerReference(tenant, ns, r.Scheme)
		},
	)

	return err
}

// 👉 Namespace bị xóa tay → tự tạo lại
// 👉 Tenant bị xóa → Namespace bị GC

func (r *TenantReconciler) ensureResourceQuota(
	ctx context.Context,
	tenant *platformv1alpha1.Tenant,
) error {

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-quota",
			Namespace: "tenant-" + tenant.Name,
		},
	}

	_, err := controllerutil.CreateOrUpdate(
		ctx,
		r.Client,
		quota,
		func() error {
			quota.Spec.Hard = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("20"),
			}

			return ctrl.SetControllerReference(tenant, quota, r.Scheme)
		},
	)

	return err
}

// 👉 Quota bị sửa tay → controller sửa ngược lại
// 👉 Đây chính là State Reconciliation
func (r *TenantReconciler) ensureNetworkPolicy(
	ctx context.Context,
	tenant *platformv1alpha1.Tenant,
) error {

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny",
			Namespace: "tenant-" + tenant.Name,
		},
	}

	_, err := controllerutil.CreateOrUpdate(
		ctx,
		r.Client,
		policy,
		func() error {
			policy.Spec = networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				},
			}

			return ctrl.SetControllerReference(tenant, policy, r.Scheme)
		},
	)

	return err
}

// 👉 Namespace mới tạo → mặc định bị deny network
// 👉 Chuẩn security-by-default
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.Info("Reconciling Tenant", "name", req.NamespacedName)

	var tenant platformv1alpha1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2️⃣ Ensure Namespace
	if err := r.ensureNamespace(ctx, &tenant); err != nil {
		return ctrl.Result{}, err
	}

	// 3️⃣ Ensure ResourceQuota
	if err := r.ensureResourceQuota(ctx, &tenant); err != nil {
		return ctrl.Result{}, err
	}

	// 4️⃣ Ensure NetworkPolicy
	if err := r.ensureNetworkPolicy(ctx, &tenant); err != nil {
		return ctrl.Result{}, err
	}

	// Only manage namespace-isolated tenants.
	if tenant.Spec.Isolation != "namespace" {
		return ctrl.Result{}, nil
	}

	tenantNS := "tenant-" + tenant.Name

	// Ensure Namespace exists.
	var ns corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: tenantNS}, &ns); err != nil {
		if apierrors.IsNotFound(err) {
			ns = corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: tenantNS,
				},
			}
			if err := r.Create(ctx, &ns); err != nil && !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, fmt.Errorf("failed to create namespace %q: %w", tenantNS, err)
			}
			log.Info("Created tenant namespace", "tenant", tenant.Name, "namespace", tenantNS)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get namespace %q: %w", tenantNS, err)
	}

	// Ensure: when Namespace is deleted, Tenant is garbage-collected.
	// OwnerReference requires Namespace UID (available after Namespace exists).
	changed := false
	refs := tenant.GetOwnerReferences()

	found := false
	for i := range refs {
		if refs[i].APIVersion == "v1" && refs[i].Kind == "Namespace" && refs[i].Name == ns.Name {
			found = true
			if refs[i].UID != ns.UID {
				refs[i].UID = ns.UID
				changed = true
			}
			break
		}
	}
	if !found {
		refs = append(refs, metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Namespace",
			Name:       ns.Name,
			UID:        ns.UID,
		})
		changed = true
	}

	if changed {
		tenant.SetOwnerReferences(refs)
		if err := r.Update(ctx, &tenant); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update tenant ownerReferences: %w", err)
		}
		log.Info("Updated Tenant ownerReference to Namespace", "tenant", tenant.Name, "namespace", tenantNS)
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *TenantReconciler) Start(ctx context.Context) error {
	log := logf.Log.WithName("tenant-signature-scanner")

	// Periodic scan interval. Keep it reasonably large to avoid hammering Rekor/registry.
	// You can override via env, e.g. SHIELDX_SIGNATURE_SCAN_INTERVAL=2m
	interval := 20 * time.Second
	err2s := notify.SendMessageTelegram("Bắt đầu scan chữ ký hình ảnh với khoảng thời gian: " + interval.String())
	if err2s != nil {
		// Don't block the admission request if Telegram is down/misconfigured.
		log.Error(err2s, "failed to send Telegram notification about signature scan start")
	}

	if v := strings.TrimSpace(strings.ToLower(getenv("SHIELDX_SIGNATURE_SCAN_INTERVAL", ""))); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		} else {
			log.Error(err, "invalid SHIELDX_SIGNATURE_SCAN_INTERVAL; using default", "value", v, "default", interval.String())
		}
	}

	log.Info("starting periodic image signature enforcement", "interval", interval.String())
	t := time.NewTicker(interval)
	defer t.Stop()

	// Run once at startup, then on each tick.
	if err := r.scanAndEnforcePodImages(ctx); err != nil {
		log.Error(err, "initial scan failed")
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("stopping periodic image signature enforcement")
			return nil
		case <-t.C:
			if err := r.scanAndEnforcePodImages(ctx); err != nil {
				log.Error(err, "periodic scan failed")
			}
		}
	}
}

func (r *TenantReconciler) scanAndEnforcePodImages(ctx context.Context) error {
	log := logf.Log.WithName("tenant-signature-scanner")

	var tenants platformv1alpha1.TenantList
	if err := r.List(ctx, &tenants); err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	for _, tenant := range tenants.Items {
		if strings.TrimSpace(strings.ToLower(tenant.Spec.Isolation)) != "namespace" {
			continue
		}
		tenantNS := "tenant-" + tenant.Name

		var pods corev1.PodList
		if err := r.List(ctx, &pods, client.InNamespace(tenantNS)); err != nil {
			// Namespace may not exist yet; don't fail the entire scan.
			if apierrors.IsNotFound(err) {
				continue
			}
			log.Error(err, "failed to list pods in tenant namespace", "tenant", tenant.Name, "namespace", tenantNS)
			continue
		}

		for i := range pods.Items {
			pod := &pods.Items[i]
			if pod.DeletionTimestamp != nil {
				continue
			}

			images := collectPodImages(pod)
			for _, image := range images {
				if strings.TrimSpace(image) == "" {
					continue
				}
				if err := verifyimage.VerifyImageSignature(image); err == nil {
					continue
				} else {
					// Enforcement action: delete pod immediately.
					delErr := r.Delete(ctx, pod, client.GracePeriodSeconds(0))
					if delErr != nil && !apierrors.IsNotFound(delErr) {
						log.Error(delErr, "failed to delete non-compliant pod", "tenant", tenant.Name, "namespace", tenantNS, "pod", pod.Name, "image", image)
					} else {
						log.Info("deleted non-compliant pod (signature verification failed)", "tenant", tenant.Name, "namespace", tenantNS, "pod", pod.Name, "image", image)
					}

					msg := fmt.Sprintf(
						"[ShieldX] Deleted pod due to image signature verification failure\nTenant: %s\nNamespace: %s\nPod: %s\nImage: %s\nError: %v",
						tenant.Name,
						tenantNS,
						pod.Name,
						image,
						err,
					)
					if nerr := notify.SendMessageTelegram(msg); nerr != nil {
						log.Error(nerr, "failed to send Telegram notification", "tenant", tenant.Name, "namespace", tenantNS, "pod", pod.Name)
					}

					// One failing image is enough to delete the pod; don't spam per container.
					break
				}
			}
		}
	}

	return nil
}

func collectPodImages(pod *corev1.Pod) []string {
	seen := make(map[string]struct{}, 8)
	out := make([]string, 0, 8)

	add := func(img string) {
		img = strings.TrimSpace(img)
		if img == "" {
			return
		}
		if _, ok := seen[img]; ok {
			return
		}
		seen[img] = struct{}{}
		out = append(out, img)
	}

	for _, c := range pod.Spec.InitContainers {
		add(c.Image)
	}
	for _, c := range pod.Spec.Containers {
		add(c.Image)
	}
	return out
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// SetupWithManager sets up the controller with the Manager.
func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Start the periodic enforcement loop alongside the controller.
	if err := mgr.Add(r); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Tenant{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("tenant").
		Complete(r)
}
