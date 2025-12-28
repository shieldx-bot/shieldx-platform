# ShieldX Tenant Platform — Kiến trúc + Quy trình (Tổng hợp)

> **Mục đích tài liệu:** Kết hợp **Kiến trúc hệ thống** và **Quy trình xây dựng - triển khai** thành một guide duy nhất, vừa đủ chi tiết để dev team bắt tay code, vừa đủ hệ thống để làm playbook vận hành.

---

## 1. Executive summary

ShieldX Tenant Platform (gọi tắt `shieldx-platform`) là một **Internal Developer Platform (IDP)** chạy trên Kubernetes, cho phép dev/team tạo môi trường đầy đủ, an toàn, và có governance chỉ bằng 1 lệnh CLI hoặc 1 CR file. Nguồn sự thật là **Tenant CRD**. Controller chịu trách nhiệm chuyển Tenant → tài nguyên Kubernetes (Namespace, RBAC, NetworkPolicy, ResourceQuota, labels để kích hoạt ImagePolicy webhook...).

Tài liệu này mô tả:

* Kiến trúc tổng thể
* Data model (CRD)
* Luồng reconcile chi tiết (pseudo-code)
* Template tài nguyên (NetworkPolicy, Quota, RoleBinding)
* UX: CLI + GitOps
* Quy trình phát triển theo Sprint + kiểm thử + vận hành

---

## 2. Kiến trúc tổng thể (Architecture)

### 2.1 Thành phần chính

* **Tenant CRD** — Abstraction: owner, tier, isolation
* **Tenant Controller** — Reconcile, Self-healing, Ownership/Watch
* **Image Policy Webhook** — Enforcement (Cosign/sigstore) — sử dụng label từ controller
* **shieldctl (CLI)** — UX cho developers
* **GitOps (optional)** — Tenant CR có thể commit vào repo (ArgoCD / Flux)
* **Observability stack** — Prometheus, Grafana, EFK/Datadog logs

### 2.2 Sơ đồ luồng (Mermaid)

```mermaid
flowchart TD
  Dev["Developer or CI"]
  CLI["shieldctl or Git commit"]
  API["Kubernetes API Server"]
  TenantCR["Tenant Custom Resource"]
  Controller["Tenant Controller"]
  Namespace["Namespace tenant-name"]
  RBAC["RoleBinding"]
  NetPol["NetworkPolicy"]
  Quota["ResourceQuota"]
  Webhook["ImagePolicy Webhook"]

  Dev -->|create tenant| CLI
  CLI --> API
  API --> TenantCR
  TenantCR --> Controller
  Controller --> Namespace
  Controller --> RBAC
  Controller --> NetPol
  Controller --> Quota
  Controller --> Webhook
  Webhook -->|admission review| API

```

### 2.3 Quy tắc thiết kế

* **Secure-by-default:** deny-all network, enforce signed images, least-privilege RBAC
* **Declarative:** Desired state = Tenant CR
* **Idempotent Reconciliation:** mỗi lần reconcile là một "audit" và có thể chạy nhiều lần
* **Separation of concerns:** Controller quản lý resources & labels; Webhook thực hiện enforcement runtime

---

## 3. Data Model — Tenant CRD

```yaml
apiVersion: platform.shieldx.io/v1alpha1
kind: Tenant
metadata:
  name: payment-team
spec:
  owners:
    - alice@company.com
    - bob@company.com
  tier: Gold        # Gold | Silver | Bronze
  isolation: Strict # Strict | Shared
  network:
    allowOutbound: false # override (careful)
  quotas: # optional overrides
    cpu: "10"
    memory: "32Gi"
```

### Giải thích

* `owners`: list emails (sẽ map sang Subject của RBAC, có thể là OIDC group)
* `tier`: determines ResourceQuota + LimitRange
* `isolation`: Strict = deny-all ingress/egress except intra-namespace; Shared = allow limited cross-namespace via NetworkPolicy
* `network.allowOutbound`: optional để cung cấp controlled egress

### Status (Suggestion)

```yaml
status:
  phase: Pending | Ready | Error
  namespace: tenant-payment-team
  conditions:
    - type: NamespaceReady
      status: "True"
    - type: NetworkPolicyReady
      status: "True"
```

---

## 4. Tenant Controller — Luồng Reconcile chi tiết

### 4.1 Các resources "Owned" (child resources)

* `Namespace` -> name: `tenant-<tenant.Name>`
* `RoleBinding` -> name: `tenant-admins` (Role: namespace-admin) hoặc ClusterRoleBinding nếu cần
* `NetworkPolicy` -> name: `default-deny` / `strict-isolation`
* `ResourceQuota` -> name: `quota-tier-<tier>`
* `LimitRange` -> name: `limits-tier-<tier>`
* `ConfigMap`/`Secret` -> nếu cần để lưu policy metadata
* Labels: `security.shieldx.io/policy: enforce`

> Controller phải `SetControllerReference(tenant, child, scheme)` cho tất cả child resources

### 4.2 Reconcile pseudo-code (chi tiết)

```go
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
  var tenant platformv1.Tenant
  if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
    if apierrors.IsNotFound(err) {
      // Tenant deleted -> nothing to do (GC will remove owned resources)
      return ctrl.Result{}, nil
    }
    return ctrl.Result{}, err
  }

  // 1. Ensure namespace
  nsName := fmt.Sprintf("tenant-%s", tenant.Name)
  ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
  if _, err := ctrl.CreateOrPatch(ctx, r.Client, ns, func() error {
    // set labels/annotations
    return controllerutil.SetControllerReference(&tenant, ns, r.Scheme)
  }); err != nil {
    r.recorder.Event(&tenant, "Warning", "NamespaceFailed", err.Error())
    return ctrl.Result{RequeueAfter: time.Minute}, err
  }

  // 2. Ensure RoleBinding for owners
  // map tenant.spec.owners -> subjects

  // 3. Ensure NetworkPolicy
  // choose template by tenant.spec.isolation

  // 4. Ensure ResourceQuota & LimitRange

  // 5. Ensure label to trigger ImagePolicy Webhook

  // 6. Update status

  return ctrl.Result{}, nil
}
```

### 4.3 Event handling & Watches

Trong `SetupWithManager`:

```go
ctrl.NewControllerManagedBy(mgr).
  For(&platformv1.Tenant{}).
  Owns(&corev1.Namespace{}).
  Owns(&netv1.NetworkPolicy{}).
  Owns(&rbacv1.RoleBinding{}).
  Complete(r)
```

**Ý nghĩa:** khi child resource bị thay đổi (kubectl edit), Controller sẽ nhận event và reconcile tenant — điều này giải thích cách revert xảy ra ngay lập tức.

---

## 5. Templates chi tiết (NetworkPolicy, ResourceQuota, RoleBinding)

### 5.1 NetworkPolicy — Strict (deny all except intra-namespace)

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: strict-isolation
  namespace: tenant-payment-team
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
      - podSelector: {} # same namespace
  egress:
    - to:
      - podSelector: {} # same namespace
```

> Optionally: allow DNS egress to kube-dns or HTTP proxy if needed (whitelist)

### 5.2 ResourceQuota — Gold example

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: quota-tier-Gold
  namespace: tenant-payment-team
spec:
  hard:
    requests.cpu: "10"
    requests.memory: "32Gi"
    limits.cpu: "20"
    limits.memory: "64Gi"
    pods: "100"
```

### 5.3 RoleBinding — Owners as namespace-admins

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tenant-admins
  namespace: tenant-payment-team
subjects:
  - kind: User
    name: alice@company.com
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: admin
  apiGroup: rbac.authorization.k8s.io
```

> Thực tế bạn có thể tạo một `TenantAdmin` ClusterRole tùy chỉnh với scope hạn chế hơn admin.

---

## 6. Security Integration — Image Policy & Webhook

* Controller chỉ cần **gắn label** `security.shieldx.io/policy=enforce` trên namespace
* ImagePolicy Webhook (đã có) `Owns()` ClusterImagePolicy và check label trên admission request
* Webhook dùng Cosign public key để verify image digest signatures

**Lý do tách:** Controller không cần logic checksum/sigverify — single responsibility.

---

## 7. UX: shieldctl CLI & GitOps

### 7.1 shieldctl (CLI) — spec

Commands:

* `shieldctl create tenant --name NAME --tier TIER --owners a,b` => create Tenant CR
* `shieldctl status tenant NAME` => show status
* `shieldctl delete tenant NAME` => delete Tenant CR (and GC children)

Behavior:

* CLI validates input
* CLI can map `owners` (emails) -> k8s subjects via OIDC mapping (configurable)
* CLI watches Tenant.status and prints spinner + step-by-step readiness

### 7.2 GitOps flow (optional)

* Tenant CR can be created by committing YAML vào repo `infrastructure/tenants/payment-team.yaml`
* ArgoCD/Flux sync sẽ apply Tenant CR
* Controller reconcile như bình thường

**Lưu ý:** với GitOps, quyền commit cần được kiểm soát (PR reviews, CODEOWNERS)

---

## 8. CI/CD & Testing

### 8.1 Unit tests

* Reconciler unit tests bằng `envtest` (controller-runtime envtest)
* Fake client để test create/patch logic

### 8.2 Integration tests

* Spin up KinD cluster trong CI
* Apply controller image
* Apply Tenant CR và assert child resources exist
* Simulate drift (kubectl patch) và assert revert

### 8.3 E2E tests

* Full flow: shieldctl -> Tenant CR -> create namespace -> deploy sample app -> ensure image policy blocks unsigned image

---

## 9. Observability & Alerts

### Metrics

* Reconcile count, duration
* Errors per Tenant
* Resource creation failures

Expose via Prometheus metrics from Controller.

### Logging

* Structured logs (tenantName, operation, reason)
* Audit events emitted via `r.recorder.Event()`

### Alerts

* Tenant reconciliation failures > threshold
* ImagePolicy webhook failures
* Unexpected resource deletions

---

## 10. Failure Modes & Operational Runbook

### 10.1 Failure: Namespace deleted by hand

**Symptom:** Tenant.status shows NamespaceMissing
**Remediation:** Controller will re-create namespace automatically (reconcile). If it fails - check controller logs for RBAC permissions and reconcile errors.

### 10.2 Failure: NetworkPolicy edited to allow egress

**Symptom:** Security audit detects unexpected egress
**Remediation:** Controller will receive event and revert. If not, check controller permissions and ensure it's `Owns(NetworkPolicy)`

### 10.3 Failure: Webhook is down

**Symptom:** unsigned images may be allowed
**Remediation:** Alerting on webhook health. Fallback: Controller can annotate namespace as `quarantine=true` and set AdmissionPolicy to deny by default (cluster admin action)

---

## 11. Rollout Plan & Phased Deployment (safest approach)

1. **Alpha** - internal: enable platform in a non-critical namespace, only for a pilot team
2. **Beta** - extend to handful teams, enable observability & strict alerts
3. **GA** - enable for all teams; add CLI + docs + onboarding

Each phase requires: runbook, rollback plan (delete Tenant CR), and postmortem template.

---Tốt, phần này mình sẽ **viết lại ở mức “thực thi được ngay”**, đúng kiểu **Sprint plan cho Platform team**, không phải checklist chung chung.
Mỗi Sprint sẽ có:

* 🎯 **Mục tiêu kỹ thuật**
* 🧠 **Tư duy thiết kế bắt buộc**
* 🧩 **Task breakdown chi tiết (theo ngày / logic)**
* ✅ **Acceptance Criteria (có thể test / demo / phá hoại)**
* ⚠️ **Anti-pattern cần tránh** (rất quan trọng)

---

# 12. Sprint Plan — ShieldX Tenant Platform

⏱️ **Tổng thời gian: 6 tuần (3 Sprint × 2 tuần)**
👥 **Giả định:** 1–2 Platform Engineers

---

## 🟦 Sprint 1 — Abstraction & Provisioning

**Thời gian:** 2 tuần
**Chủ đề:** *Foundation & Controller correctness*

> 👉 Sprint này quyết định **bạn có phải Platform Engineer thật hay không**
> Nếu làm sai Sprint 1 → các Sprint sau sẽ thành nợ kỹ thuật.

---

## 🎯 Mục tiêu Sprint 1

* Thiết lập **Tenant CRD** làm *Single Source of Truth*
* Controller **có thể reconcile chuẩn**, idempotent
* Namespace lifecycle **được kiểm soát hoàn toàn**
* Có **unit test chứng minh self-healing**

---

## 🧠 Tư duy thiết kế bắt buộc

* **Controller ≠ Script**
* Reconcile có thể chạy:

  * nhiều lần
  * bất kỳ lúc nào
  * trong trạng thái cluster bị phá
* Không được:

  * giả định namespace đã tồn tại
  * giả định thứ tự tạo tài nguyên

---

## 🧩 Task Breakdown — Sprint 1

### 🔹 Task 1.1 — Scaffold Kubebuilder project

**Việc làm**

```bash
kubebuilder init \
  --domain shieldx.io \
  --repo github.com/shieldx-bot/shieldx-platform \
  --plugins go/v4
```

**Kết quả**

* Có cấu trúc chuẩn:

  ```
  api/
  controllers/
  cmd/manager/
  config/
  ```

**Checklist**

* Manager chạy được (Done)
* CRD có thể apply vào cluster (Done)

---

### 🔹 Task 1.2 — Define Tenant CRD + Status

**Việc làm**

* Tạo API:

  ```
  platform.shieldx.io/v1alpha1
  Tenant
  ```
* `spec` chỉ chứa **business intent**
* `status` phản ánh **tình trạng hệ thống**

**TenantSpec tối thiểu**

```go
type TenantSpec struct {
  Owners    []string `json:"owners"`
  Tier      string   `json:"tier"`
  Isolation string   `json:"isolation"`
}
```

**TenantStatus**

```go
type TenantStatus struct {
  Phase      string `json:"phase"`
  Namespace  string `json:"namespace"`
}
```

**Checklist**

* `make manifests` (Done) 
* `kubectl apply -f config/crd` (Done)
* `kubectl get tenants` (Done)

---

### 🔹 Task 1.3 — Implement Namespace Provisioning

**Logic bắt buộc**

* Namespace name: `tenant-<tenant.Name>`
* Phải dùng:

  * `controllerutil.CreateOrPatch`
  * `SetControllerReference`

**Pseudo-flow**

```
IF namespace not found
  CREATE namespace
ELSE
  ENSURE labels / ownerref correct
```

**Checklist**

* Namespace tự tạo
* Có ownerReference trỏ về Tenant

---

### 🔹 Task 1.4 — OwnerReference & Self-Healing

**Việc làm**

* Gắn OwnerReference:

  ```
  Tenant -> Namespace
  ```

**Tình huống phải xử lý**

1. `kubectl delete namespace tenant-x`
2. Controller phải:

   * nhận event
   * tạo lại namespace

**Checklist**

* Không panic
* Không loop vô hạn
* Namespace quay lại sau vài giây

---

### 🔹 Task 1.5 — Unit Tests (envtest)

**Việc làm**

* Dùng `controller-runtime/envtest`
* Test case tối thiểu:

```text
Given: Tenant created
Then: Namespace exists
And: Namespace.ownerRef == Tenant
```

```text
Given: Tenant deleted
Then: Namespace is garbage-collected
```

**Checklist**

* Test chạy trong CI
* Không cần cluster thật

---

## ✅ Acceptance Criteria — Sprint 1

* ✅ Tạo Tenant → namespace xuất hiện
* ✅ Namespace có OwnerReference đúng
* ✅ Xoá Tenant → namespace tự biến mất
* ✅ Xoá namespace → controller tạo lại
* ✅ Unit test pass

---

## ⚠️ Anti-patterns cần tránh

* ❌ Tạo namespace bằng `Create()` không patch
* ❌ Không set OwnerReference
* ❌ Logic phụ thuộc thứ tự chạy

---

---

## 🟨 Sprint 2 — Isolation & Governance

**Thời gian:** 2 tuần
**Chủ đề:** *Security & Policy Enforcement*

---

## 🎯 Mục tiêu Sprint 2

* Áp chính sách **Zero Trust Networking**
* Quản lý **tài nguyên theo tier**
* RBAC chính xác theo `spec.owners`
* Chứng minh **không thể bypass bằng kubectl**

---

## 🧠 Tư duy thiết kế bắt buộc

* Security **không dựa vào con người**
* Mọi policy:

  * phải declarative
  * phải reconcile liên tục

---

## 🧩 Task Breakdown — Sprint 2

### 🔹 Task 2.1 — NetworkPolicy templates

**Logic**

* Nếu `isolation=Strict`
  → tạo NetworkPolicy deny-all ingress + egress

**Bắt buộc**

* Controller **Owns NetworkPolicy**

**Checklist**

* Pod khác namespace không thể ping
* Sửa tay NetworkPolicy → bị revert

---

### 🔹 Task 2.2 — ResourceQuota theo Tier

**Mapping ví dụ**

| Tier   | CPU | Memory |
| ------ | --- | ------ |
| Gold   | 10  | 32Gi   |
| Silver | 4   | 8Gi    |

**Checklist**

* Pod vượt quota → bị reject
* Đổi tier → quota được update

---

### 🔹 Task 2.3 — RBAC từ spec.owners

**Logic**

* `owners` → `subjects`
* Tạo RoleBinding trong namespace

**Checklist**

* Owner deploy được pod
* User khác → bị forbidden

---

### 🔹 Task 2.4 — Integration Tests (KinD)

**Scenario test**

1. Tạo Tenant
2. Deploy pod từ owner → OK
3. Pod từ namespace khác → FAIL
4. Edit NetworkPolicy → revert

---

## ✅ Acceptance Criteria — Sprint 2

* ✅ isolation=Strict → deny-all network policy
* ✅ Owner có quyền admin namespace
* ✅ Namespace khác không truy cập được
* ✅ Drift bị sửa tự động

---

## ⚠️ Anti-patterns Sprint 2

* ❌ Không Owns NetworkPolicy
* ❌ Hardcode RBAC
* ❌ Không test phá hoại

---

---

## 🟩 Sprint 3 — Developer Experience & Hardening

**Thời gian:** 2 tuần
**Chủ đề:** *Adoption & Production readiness*

---

## 🎯 Mục tiêu Sprint 3

* Dev **muốn dùng platform**
* Có CI/CD chuẩn
* Có docs + ADR
* Có E2E test

---

## 🧩 Task Breakdown — Sprint 3

### 🔹 Task 3.1 — Build `shieldctl` CLI

**Commands**

```bash
shieldctl create tenant
shieldctl status tenant
shieldctl delete tenant
```

**Logic**

* CLI gọi Kubernetes API
* Không gọi controller trực tiếp

---

### 🔹 Task 3.2 — Status & UX

**Tenant.status**

* Phase: Pending → Ready
* Conditions từng bước

**CLI UX**

* Spinner
* Emoji / màu
* Progress rõ ràng

---

### 🔹 Task 3.3 — CI Pipeline

**Pipeline**

* Build controller image
* Run unit tests
* Spin KinD
* Run E2E

---

### 🔹 Task 3.4 — Documentation & ADR

**Docs**

* README
* Onboarding guide
* Architecture diagram

**ADR**

* Vì sao dùng CRD
* Vì sao không dùng Terraform

---

## ✅ Acceptance Criteria — Sprint 3

* ✅ Dev chạy CLI thấy tenant READY
* ✅ CI pass khi PR merge
* ✅ E2E test chạy được
* ✅ Có tài liệu onboard

---

## 🎯 Tổng kết

Nếu hoàn thành đủ 3 Sprint này, bạn **không chỉ học Kubernetes Operator**.

Bạn đã chứng minh được khả năng:

* Thiết kế **Internal Developer Platform**
* Áp dụng **Controller Pattern chuẩn**
* Xây **Security-first multi-tenant system**

👉 Đây là level **Senior / Staff Platform Engineer** thật sự.

 

---

## 13. Operational & Security Considerations

* **RBAC for Controller:** least privilege but đủ để create/patch/owner resources
* **Secrets handling:** if controller needs to write secrets, use sealed-secrets or external vault
* **Audit logging:** enable kubernetes audit logs for Tenant actions
* **Policy drift:** run periodic scans comparing Tenant.spec ↔ actual resources
* **Escalation path:** break-glass: cluster-admin can disable controller via feature-flag ConfigMap

---

## 14. Deliverables & Artefacts

* `shieldx-platform` repo with modules:

  * `api/` (Tenant types)
  * `controllers/` (reconciler)
  * `cmd/manager` (operator binary)
  * `cli/` (shieldctl)
  * `deploy/` (helm/chart, manifests)
  * `tests/` (unit/integration/e2e)
* ADR: decision on tier sizes, network model, RBAC model
* Runbook & Onboarding guide

---

## 15. Next steps (Suggested immediate tasks)

1. Scaffold repo with Kubebuilder (module layout)
2. Implement Tenant type + CRD YAML
3. Implement basic Reconcile: create namespace + ownerref + status update
4. Add unit tests with envtest for case: create tenant -> ns created

Bạn muốn mình **tạo sẵn** scaffold code (kubebuilder layout + sample reconciler) hoặc **viết pseudo-code chi tiết** cho từng task của Sprint 1? Chỉ cần chọn: `Scaffold` hoặc `Pseudo-code Sprint 1`.
