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

package v1alpha1

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/shieldx-bot/shieldx-platform/api/v1alpha1"
	"github.com/shieldx-bot/shieldx-platform/internal/webhook/notify"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint:unused
// log is for logging in this package.
var tenantlog = logf.Log.WithName("tenant-resource")

// SetupTenantWebhookWithManager registers the webhook for Tenant in the manager.
func SetupTenantWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&platformv1alpha1.Tenant{}).
		WithValidator(&TenantCustomValidator{}).
		WithDefaulter(&TenantCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-platform-shieldx-io-v1alpha1-tenant,mutating=true,failurePolicy=fail,sideEffects=None,groups=platform.shieldx.io,resources=tenants,verbs=create;update,versions=v1alpha1,name=mtenant-v1alpha1.kb.io,admissionReviewVersions=v1

// TenantCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Tenant when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TenantCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &TenantCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Tenant.
func (d *TenantCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	tenant, ok := obj.(*platformv1alpha1.Tenant)

	if !ok {
		return fmt.Errorf("expected an Tenant object but got %T", obj)
	}
	tenantlog.Info("Defaulting for Tenant", "name", tenant.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-platform-shieldx-io-v1alpha1-tenant,mutating=false,failurePolicy=fail,sideEffects=None,groups=platform.shieldx.io,resources=tenants,verbs=create;update;delete,versions=v1alpha1,name=vtenant-v1alpha1.kb.io,admissionReviewVersions=v1

// TenantCustomValidator struct is responsible for validating the Tenant resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TenantCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &TenantCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Tenant.
func (v *TenantCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	tenant, ok := obj.(*platformv1alpha1.Tenant)
	tenantlog.Info("Webhook đã được gọi khi tạo Tenant")
	if !ok {
		return nil, fmt.Errorf("expected a Tenant object but got %T", obj)
	}
	err0 := notify.SendMessageTelegram("Phiên bản v0.0.13")
	if err0 != nil {
		// Don't block the admission request if Telegram is down/misconfigured.
		tenantlog.Error(err0, "failed to send Telegram notification", "tenant", tenant.GetName())
	}

	// err := k8s.CreateReconciliation(tenant.GetName(), tenant.Spec.Tier, tenant.Spec.Isolation, tenant.Spec.Owners, tenant.Spec.ResourceQuota, tenant.Spec.NetworkPolicy)
	// if err != nil {
	// 	tenantlog.Error(err, "failed to create reconciliation", "tenant", tenant.GetName())
	// 	return nil, err
	// }

	tenantlog.Info("Validation for Tenant upon creation", "name", tenant.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Tenant.
func (v *TenantCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	tenant, ok := newObj.(*platformv1alpha1.Tenant)
	if !ok {
		return nil, fmt.Errorf("expected a Tenant object for the newObj but got %T", newObj)
	}
	tenantlog.Info("Validation for Tenant upon update", "name", tenant.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Tenant.
func (v *TenantCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	tenant, ok := obj.(*platformv1alpha1.Tenant)
	if !ok {
		return nil, fmt.Errorf("expected a Tenant object but got %T", obj)
	}
	err1 := notify.SendMessageTelegram("Xóa Tenant: " + tenant.GetName())
	if err1 != nil {
		// Don't block the admission request if Telegram is down/misconfigured.
		tenantlog.Error(err1, "failed to send Telegram notification", "tenant", tenant.GetName())
	}
	// err := k8s.DeleleteReconciliation(tenant.GetName())
	// if err != nil {
	// 	tenantlog.Error(err, "failed to delete reconciliation", "tenant", tenant.GetName())
	// 	return nil, err
	// }
	tenantlog.Info("Validation for Tenant upon deletion", "name", tenant.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
