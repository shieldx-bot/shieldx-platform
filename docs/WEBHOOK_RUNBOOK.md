# Runbook: Build → Deploy → Đăng ký webhook → Smoke test (ShieldX Platform)

Tài liệu này là “playbook” chạy lệnh end-to-end cho dự án Kubebuilder/controller-runtime trong repo `shieldx-platform`.

> Mục tiêu:
> 1) Build & publish image để cluster pull được
> 2) Deploy CRD + controller + webhook service + webhook configurations
> 3) Xác nhận webhook server chạy (TLS OK, endpoint OK)
> 4) Smoke test admission webhook bằng **server-side dry-run**
>
> Namespace mặc định của hệ thống trong repo này là: `shieldx-platform-system`.

---

## 0) Chuẩn bị (an toàn, giảm lỗi)

### Yêu cầu

- `go`, `kubectl`
- Docker/Podman (repo mặc định dùng Docker)
- Cluster Kubernetes:
  - Local: Kind (dễ test), hoặc
  - Remote: GKE/EKS/AKS (cần image registry + quyền pull)

### Kiểm tra nhanh cluster

```bash
kubectl cluster-info
kubectl get ns shieldx-platform-system
```

Nếu `shieldx-platform-system` chưa tồn tại, chạy `make deploy` sẽ tạo.

### Preflight (tránh lỗi setup hay gặp)

```bash
# Kustomize phải có (kubectl bản mới thường có sẵn: kubectl kustomize)
command -v kustomize >/dev/null && kustomize version || kubectl kustomize --help >/dev/null

# Cert-manager CRDs phải tồn tại nếu bạn deploy theo đúng config/default (repo đang enable ../certmanager)
kubectl get crd | egrep -i 'certificates\.cert-manager\.io|issuers\.cert-manager\.io' || true

# Nếu không thấy CRD cert-manager: bạn phải cài cert-manager trước, hoặc tắt phần ../certmanager trong config/default/kustomization.yaml
```

---

## 1) Build & Push image (điều bắt buộc nếu cluster remote)

### 1.1 Chọn image tag (khuyến nghị dùng tag theo commit)

```bash
export IMG=shieldxbot/controller:$(git rev-parse --short HEAD)
# hoặc (nhanh nhưng dễ cache/pull nhầm)
# export IMG=shieldxbot/controller:latest
```

> Lưu ý quan trọng: nếu bạn dùng cluster remote mà để image local kiểu `controller:latest` thì controller pod thường sẽ **ImagePullBackOff**.

### 1.2 Build & push

Repo đã có target:

```bash
make docker-build IMG=$IMG
```

Sau bước này:
- Image phải tồn tại trên registry
- Cluster phải pull được (public repo hoặc có `imagePullSecret`)

---

## 2) Deploy (CRD + controller + webhook service + webhook configuration)

```bash
make deploy IMG=$IMG
```

Deploy sẽ apply các tài nguyên quan trọng:
- CRD `tenants.platform.shieldx.io`
- Deployment controller-manager
- Service metrics + Service webhook
- MutatingWebhookConfiguration + ValidatingWebhookConfiguration
- Certificate/Issuer (cert-manager) và inject CA (nếu cấu hình)

### 2.1) Các file YAML cấu hình webhook (để “đăng ký webhook”)

Trong repo này, `make deploy` thực chất chạy **kustomize** từ `config/default`.
Vì vậy, cách **an toàn nhất để “đăng ký webhook”** (đúng namespace + đúng namePrefix + đúng CA injection) là apply kustomize output.

Copy/paste các lệnh sau:

```bash
# (Tuỳ chọn) xem toàn bộ YAML cuối cùng mà cluster sẽ nhận
kustomize build config/default

# Apply toàn bộ stack (CRD + controller + webhook + cert-manager manifests trong repo)
kubectl apply -k config/default

# Hoặc: apply bằng cách stream YAML (tiện nếu bạn muốn lưu/inspect)
# kustomize build config/default | kubectl apply -f -
```

Các “YAML nguồn” mà kustomize sẽ kéo vào để đăng ký webhook (tham khảo nhanh) gồm Service + WebhookConfigurations + Issuer/Certificate + patch mount cert.
Nếu bạn muốn mở ra xem nhanh từng file, dùng luôn các lệnh dưới đây:

```bash
# Webhook Service (443 -> 9443)
sed -n '1,200p' config/webhook/service.yaml

# MutatingWebhookConfiguration + ValidatingWebhookConfiguration
sed -n '1,240p' config/webhook/manifests.yaml

# cert-manager: Issuer + Certificate (tạo secret webhook-server-cert)
sed -n '1,200p' config/certmanager/issuer.yaml
sed -n '1,240p' config/certmanager/certificate-webhook.yaml

# Patch: mount TLS secret vào controller-manager + mở port 9443
sed -n '1,240p' config/default/manager_webhook_patch.yaml
```

> Lưu ý: các file “nguồn” phía trên có thể đang dùng namespace `system` và tên không có prefix.
> Khi apply bằng `kubectl apply -k config/default`, kustomize sẽ tự set:
> - namespace: `shieldx-platform-system`
> - namePrefix: `shieldx-platform-`
> …để ra đúng tên resource khi deploy.

Sau khi apply, bạn có thể verify nhanh webhook đã “đăng ký” bằng các lệnh copy/paste sau:

```bash
# Webhook service + endpoints
kubectl -n shieldx-platform-system get svc shieldx-platform-webhook-service -o wide
echo "---------------------------"
kubectl -n shieldx-platform-system get endpoints shieldx-platform-webhook-service -o wide
echo "---------------------------"
kubectl -n shieldx-platform-system get endpointslice -l kubernetes.io/service-name=shieldx-platform-webhook-service -o wide

# Webhook configurations (cluster-scoped)
kubectl get validatingwebhookconfigurations shieldx-platform-validating-webhook-configuration -o yaml | sed -n '1,160p'
echo "---------------------------"
kubectl get mutatingwebhookconfigurations shieldx-platform-mutating-webhook-configuration -o yaml | sed -n '1,160p'

# cert-manager objects backing webhook TLS
kubectl -n shieldx-platform-system get issuer selfsigned-issuer
echo "---------------------------"
kubectl -n shieldx-platform-system get certificate serving-cert
echo "---------------------------"
kubectl -n shieldx-platform-system get secret webhook-server-cert
```

### 2.2) Toàn bộ YAML (copy/paste trực tiếp)

Các YAML dưới đây là **nguyên văn** từ repo ("YAML nguồn"). Lưu ý: chúng đang dùng `namespace: system` và tên resource chưa có prefix.
Khi bạn deploy theo chuẩn của repo bằng `kubectl apply -k config/default` thì kustomize sẽ tự đổi sang namespace `shieldx-platform-system` và thêm prefix `shieldx-platform-`.

#### Webhook Service (`config/webhook/service.yaml`)

```yaml
apiVersion: v1
kind: Service
metadata:
  labels:
    app.kubernetes.io/name: shieldx-platform
    app.kubernetes.io/managed-by: kustomize
  name: webhook-service
  namespace: system
spec:
  ports:
    - port: 443
      protocol: TCP
      targetPort: 9443
  selector:
    control-plane: controller-manager
    app.kubernetes.io/name: shieldx-platform
```

#### Webhook Configurations (`config/webhook/manifests.yaml`)

```yaml
---
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: mutating-webhook-configuration
webhooks:
- admissionReviewVersions:
  - v1
  clientConfig:
    service:
      name: webhook-service
      namespace: system
      path: /mutate-platform-shieldx-io-v1alpha1-tenant
  failurePolicy: Fail
  name: mtenant-v1alpha1.kb.io
  rules:
  - apiGroups:
    - platform.shieldx.io
    apiVersions:
    - v1alpha1
    operations:
    - CREATE
    - UPDATE
    resources:
    - tenants
  sideEffects: None
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: validating-webhook-configuration
webhooks:
- admissionReviewVersions:
  - v1
  clientConfig:
    service:
      name: webhook-service
      namespace: system
      path: /validate-platform-shieldx-io-v1alpha1-tenant
  failurePolicy: Fail
  name: vtenant-v1alpha1.kb.io
  rules:
  - apiGroups:
    - platform.shieldx.io
    apiVersions:
    - v1alpha1
    operations:
    - CREATE
    - UPDATE
    resources:
    - tenants
  sideEffects: None
```

#### cert-manager Issuer (`config/certmanager/issuer.yaml`)

```yaml
# The following manifest contains a self-signed issuer CR.
# More information can be found at https://docs.cert-manager.io
# WARNING: Targets CertManager v1.0. Check https://cert-manager.io/docs/installation/upgrading/ for breaking changes.
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  labels:
    app.kubernetes.io/name: shieldx-platform
    app.kubernetes.io/managed-by: kustomize
  name: selfsigned-issuer
  namespace: system
spec:
  selfSigned: {}
```

#### cert-manager Certificate cho webhook (`config/certmanager/certificate-webhook.yaml`)

```yaml
# The following manifests contain a self-signed issuer CR and a certificate CR.
# More document can be found at https://docs.cert-manager.io
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  labels:
    app.kubernetes.io/name: shieldx-platform
    app.kubernetes.io/managed-by: kustomize
  name: serving-cert # this name should match the one appeared in kustomizeconfig.yaml
  namespace: system
spec:
  # SERVICE_NAME and SERVICE_NAMESPACE will be substituted by kustomize
  # replacements in the config/default/kustomization.yaml file.
  dnsNames:
    - SERVICE_NAME.SERVICE_NAMESPACE.svc
    - SERVICE_NAME.SERVICE_NAMESPACE.svc.cluster.local
  issuerRef:
    kind: Issuer
    name: selfsigned-issuer
  secretName: webhook-server-cert
```

#### cert-manager Certificate cho metrics (được deploy kèm trong repo) (`config/certmanager/certificate-metrics.yaml`)

```yaml
# The following manifests contain a self-signed issuer CR and a metrics certificate CR.
# More document can be found at https://docs.cert-manager.io
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  labels:
    app.kubernetes.io/name: shieldx-platform
    app.kubernetes.io/managed-by: kustomize
  name: metrics-certs # this name should match the one appeared in kustomizeconfig.yaml
  namespace: system
spec:
  dnsNames:
    # SERVICE_NAME and SERVICE_NAMESPACE will be substituted by kustomize
    # replacements in the config/default/kustomization.yaml file.
    - SERVICE_NAME.SERVICE_NAMESPACE.svc
    - SERVICE_NAME.SERVICE_NAMESPACE.svc.cluster.local
  issuerRef:
    kind: Issuer
    name: selfsigned-issuer
  secretName: metrics-server-cert
```

#### Kustomize “glue” cho webhook (giúp auto đổi service name/namespace trong webhook configs)

`config/webhook/kustomization.yaml`

```yaml
resources:
- manifests.yaml
- service.yaml

configurations:
- kustomizeconfig.yaml
```

`config/webhook/kustomizeconfig.yaml`

```yaml
# the following config is for teaching kustomize where to look at when substituting nameReference.
# It requires kustomize v2.1.0 or newer to work properly.
nameReference:
- kind: Service
  version: v1
  fieldSpecs:
  - kind: MutatingWebhookConfiguration
    group: admissionregistration.k8s.io
    path: webhooks/clientConfig/service/name
  - kind: ValidatingWebhookConfiguration
    group: admissionregistration.k8s.io
    path: webhooks/clientConfig/service/name

namespace:
- kind: MutatingWebhookConfiguration
  group: admissionregistration.k8s.io
  path: webhooks/clientConfig/service/namespace
  create: true
- kind: ValidatingWebhookConfiguration
  group: admissionregistration.k8s.io
  path: webhooks/clientConfig/service/namespace
  create: true
```

#### Kustomize “glue” cho cert-manager (giúp update issuerRef/name)

`config/certmanager/kustomization.yaml`

```yaml
resources:
  - issuer.yaml
  - certificate-webhook.yaml
  - certificate-metrics.yaml

configurations:
  - kustomizeconfig.yaml
```

`config/certmanager/kustomizeconfig.yaml`

```yaml
# This configuration is for teaching kustomize how to update name ref substitution
nameReference:
- kind: Issuer
  group: cert-manager.io
  fieldSpecs:
  - kind: Certificate
    group: cert-manager.io
    path: spec/issuerRef/name
```

#### Metrics service/patch (repo deploy kèm trong config/default)

`config/default/metrics_service.yaml`

```yaml
apiVersion: v1
kind: Service
metadata:
  labels:
    control-plane: controller-manager
    app.kubernetes.io/name: shieldx-platform
    app.kubernetes.io/managed-by: kustomize
  name: controller-manager-metrics-service
  namespace: system
spec:
  ports:
  - name: https
    port: 8443
    protocol: TCP
    targetPort: 8443
  selector:
    control-plane: controller-manager
    app.kubernetes.io/name: shieldx-platform
```

`config/default/manager_metrics_patch.yaml`

```yaml
# This patch adds the args to allow exposing the metrics endpoint using HTTPS
- op: add
  path: /spec/template/spec/containers/0/args/0
  value: --metrics-bind-address=:8443
```

#### Kustomization tổng (đây là thứ quyết định namespace/prefix và bật webhook/cert-manager) (`config/default/kustomization.yaml`)

```yaml
# Adds namespace to all resources.
namespace: shieldx-platform-system

# Value of this field is prepended to the
# names of all resources, e.g. a deployment named
# "wordpress" becomes "alices-wordpress".
# Note that it should also match with the prefix (text before '-') of the namespace
# field above.
namePrefix: shieldx-platform-

resources:
- ../crd
- ../rbac
- ../manager
- ../webhook
- ../certmanager
- metrics_service.yaml

patches:
- path: manager_metrics_patch.yaml
  target:
    kind: Deployment

- path: manager_webhook_patch.yaml
  target:
    kind: Deployment

replacements:
 - source:
     kind: Service
     version: v1
     name: webhook-service
     fieldPath: .metadata.name
   targets:
     - select:
         kind: Certificate
         group: cert-manager.io
         version: v1
         name: serving-cert
       fieldPaths:
         - .spec.dnsNames.0
         - .spec.dnsNames.1
       options:
         delimiter: '.'
         index: 0
         create: true
 - source:
     kind: Service
     version: v1
     name: webhook-service
     fieldPath: .metadata.namespace
   targets:
     - select:
         kind: Certificate
         group: cert-manager.io
         version: v1
         name: serving-cert
       fieldPaths:
         - .spec.dnsNames.0
         - .spec.dnsNames.1
       options:
         delimiter: '.'
         index: 1
         create: true

 - source:
     kind: Certificate
     group: cert-manager.io
     version: v1
     name: serving-cert
     fieldPath: .metadata.namespace
   targets:
     - select:
         kind: ValidatingWebhookConfiguration
       fieldPaths:
         - .metadata.annotations.[cert-manager.io/inject-ca-from]
       options:
         delimiter: '/'
         index: 0
         create: true
 - source:
     kind: Certificate
     group: cert-manager.io
     version: v1
     name: serving-cert
     fieldPath: .metadata.name
   targets:
     - select:
         kind: ValidatingWebhookConfiguration
       fieldPaths:
         - .metadata.annotations.[cert-manager.io/inject-ca-from]
       options:
         delimiter: '/'
         index: 1
         create: true

 - source:
     kind: Certificate
     group: cert-manager.io
     version: v1
     name: serving-cert
     fieldPath: .metadata.namespace
   targets:
     - select:
         kind: MutatingWebhookConfiguration
       fieldPaths:
         - .metadata.annotations.[cert-manager.io/inject-ca-from]
       options:
         delimiter: '/'
         index: 0
         create: true
 - source:
     kind: Certificate
     group: cert-manager.io
     version: v1
     name: serving-cert
     fieldPath: .metadata.name
   targets:
     - select:
         kind: MutatingWebhookConfiguration
       fieldPaths:
         - .metadata.annotations.[cert-manager.io/inject-ca-from]
       options:
         delimiter: '/'
         index: 1
         create: true
```

#### Patch mount TLS cert vào controller (`config/default/manager_webhook_patch.yaml`)

```yaml
# This patch ensures the webhook certificates are properly mounted in the manager container.
# It configures the necessary arguments, volumes, volume mounts, and container ports.

# Add the --webhook-cert-path argument for configuring the webhook certificate path
- op: add
  path: /spec/template/spec/containers/0/args/-
  value: --webhook-cert-path=/tmp/k8s-webhook-server/serving-certs

# Add the volumeMount for the webhook certificates
- op: add
  path: /spec/template/spec/containers/0/volumeMounts/-
  value:
    mountPath: /tmp/k8s-webhook-server/serving-certs
    name: webhook-certs
    readOnly: true

# Add the port configuration for the webhook server
- op: add
  path: /spec/template/spec/containers/0/ports/-
  value:
    containerPort: 9443
    name: webhook-server
    protocol: TCP

# Add the volume configuration for the webhook certificates
- op: add
  path: /spec/template/spec/volumes/-
  value:
    name: webhook-certs
    secret:
      # Use the Secret issued by config/certmanager/certificate-webhook.yaml (Certificate: serving-cert)
      # This must match the CA injected into the *WebhookConfiguration caBundle.
      secretName: webhook-server-cert
```

---

## 3) Check controller chạy ổn (điều kiện để webhook hoạt động)

### 3.1 Chờ rollout

```bash
kubectl -n shieldx-platform-system rollout status deploy/shieldx-platform-controller-manager --timeout=180s
```

### 3.2 Kiểm tra pod

```bash
kubectl -n shieldx-platform-system get pods -l control-plane=controller-manager -o wide
```

**Nếu có lỗi ImagePullBackOff/ErrImagePull**:

```bash
kubectl -n shieldx-platform-system describe pod -l control-plane=controller-manager | sed -n '/Events/,$p'
```

---

## 4) Check webhook đã “đăng ký” và có endpoint (an toàn)

### 4.1 Xác nhận webhook configurations tồn tại

```bash
kubectl get validatingwebhookconfigurations | grep -i shieldx || true
kubectl get mutatingwebhookconfigurations | grep -i shieldx || true
```

### 4.2 Xác nhận webhook service có endpoints

```bash
kubectl -n shieldx-platform-system get svc shieldx-platform-webhook-service -o wide
kubectl -n shieldx-platform-system get endpoints shieldx-platform-webhook-service -o wide
kubectl -n shieldx-platform-system get endpointslice -l kubernetes.io/service-name=shieldx-platform-webhook-service -o wide
```

Kỳ vọng:
- `shieldx-platform-webhook-service` port 443 → targetPort 9443
- Endpoints/EndpointSlice có IP:9443 (vd `10.x.x.x:9443`)

> Nếu service có nhưng endpoints rỗng => admission webhook chắc chắn fail (timeout/no endpoints).

---

## 5) Check webhook server đã thực sự chạy (log)

```bash
kubectl -n shieldx-platform-system logs -l control-plane=controller-manager -c manager --tail=300 \
  | egrep -i 'Starting webhook server|Serving webhook server|Registering webhook|tls|cert|error' || true
```

Kỳ vọng thấy:
- `Starting webhook server`
- `Serving webhook server  {"port": 9443}`

---

## 6) Check TLS/CA (ngăn lỗi phổ biến: x509 unknown authority)

### 6.1 Nếu dùng cert-manager

```bash
kubectl -n shieldx-platform-system get certificate,issuer
```

### 6.2 Quy tắc vàng

- WebhookConfiguration phải có `caBundle` đúng (thường được cert-manager inject bằng annotation `cert-manager.io/inject-ca-from`).
- Deployment controller phải **mount đúng secret** chứa `tls.crt/tls.key` tương ứng với CA đó.

**Triệu chứng mount sai secret / CA không khớp**:
- `tls: failed to verify certificate: x509: certificate signed by unknown authority`

---

## 6.5) Telegram notification (nếu bạn kỳ vọng webhook “bắn” tin nhắn)

Webhook validate (Tenant) trong repo có gọi Telegram ở `ValidateCreate()`.

### 6.5.1) Nhớ phân biệt dry-run

```bash
# Webhook CHẠY (server-side dry-run => request đi qua apiserver => gọi admission webhook)
kubectl apply --dry-run=server -f your-tenant.yaml

# Webhook KHÔNG chạy (client-side dry-run => không gọi apiserver)
kubectl apply --dry-run=client -f your-tenant.yaml
```

### 6.5.2) Verify pod đang chạy đúng image (tránh “local đã sửa nhưng cluster vẫn chạy bản cũ”)

```bash
kubectl -n shieldx-platform-system get pods -l control-plane=controller-manager \
  -o custom-columns=NAME:.metadata.name,IMAGE:.spec.containers[0].image,IMAGEID:.status.containerStatuses[0].imageID
```

Khuyến nghị: tránh dùng `:latest` khi debug. Dùng tag theo commit rồi deploy lại:

```bash
export IMG=shieldxbot/controller:$(git rev-parse --short HEAD)
make docker-build IMG=$IMG
make deploy IMG=$IMG
```

### 6.5.3) Cấu hình Telegram credentials (Secret)

Repo kỳ vọng Secret `telegram-credentials` có **đúng 2 key**: `botToken`, `chatId` (namespace `shieldx-platform-system`).

```bash
kubectl -n shieldx-platform-system get secret telegram-credentials -o yaml || true
```

Nếu bạn muốn tạo lại secret “sạch” (tránh quotes/newline) từ env local:

```bash
kubectl -n shieldx-platform-system delete secret telegram-credentials --ignore-not-found=true

kubectl -n shieldx-platform-system create secret generic telegram-credentials \
  --from-literal=botToken="$TELEGRAM_BOT_TOKEN" \
  --from-literal=chatId="$TELEGRAM_CHAT_ID"

kubectl -n shieldx-platform-system rollout restart deploy/shieldx-platform-controller-manager
kubectl -n shieldx-platform-system rollout status deploy/shieldx-platform-controller-manager --timeout=180s
```

### 6.5.4) So khớp token trong cluster với token local (không lộ token)

Nếu local `go run` gửi được nhưng webhook trong cluster trả `401 Unauthorized`, gần như chắc chắn **token trong pod khác token local**.
So khớp bằng **độ dài** và **sha256** để không in token ra màn hình:

```bash
# Hash/length token trong Kubernetes Secret
kubectl -n shieldx-platform-system get secret telegram-credentials -o jsonpath='{.data.botToken}' \
  | base64 -d \
  | tee >(wc -c | awk '{print "k8s_token_len_bytes=" $1}') \
  | sha256sum \
  | awk '{print "k8s_token_sha256=" $1}'

# Hash/length token local đang dùng
printf %s "$TELEGRAM_BOT_TOKEN" \
  | tee >(wc -c | awk '{print "local_token_len_bytes=" $1}') \
  | sha256sum \
  | awk '{print "local_token_sha256=" $1}'
```

### 6.5.5) Check log khi không thấy Telegram gửi về

```bash
kubectl -n shieldx-platform-system logs -l control-plane=controller-manager -c manager --tail=400 \
  | egrep -i 'Telegram API returned non-2xx|Unauthorized|failed to send Telegram|telegram bot token' || true

kubectl -n shieldx-platform-system logs -l control-plane=controller-manager -c manager --tail=400 \
  | egrep -i 'Defaulting for Tenant|Validation for Tenant upon creation' || true
```

Các lỗi hay gặp:

- `401 Unauthorized` ⇒ bot token sai / token bị revoke / token copy thiếu ký tự.
- `telegram bot token or chat ID is not set...` ⇒ secret thiếu key `botToken` hoặc `chatId`, hoặc pod chưa restart để nhận env mới.
- timeout / DNS / egress/network policy ⇒ cluster không outbound được tới `api.telegram.org`.

---

## 7) Smoke test webhook (chuẩn nhất: server-side dry-run)

### 7.1 Test object hợp lệ (không tạo thật)

```bash
kubectl apply --dry-run=server -f - <<'YAML'
apiVersion: platform.shieldx.io/v1alpha1
kind: Tenant
metadata:
  name: tenant-webhook-smoketest
spec:
  owners:
    - admin@example.com
  tier: basic
  isolation: namespace
YAML
```

Kỳ vọng:
- `created (server dry run)` hoặc `configured (server dry run)`

### 7.2 Test object sai để chắc webhook validate đang chạy

```bash
kubectl apply --dry-run=server -f - <<'YAML'
apiVersion: platform.shieldx.io/v1alpha1
kind: Tenant
metadata:
  name: tenant-webhook-should-fail
spec:
  tier: basic
  isolation: namespace
YAML
```

Kỳ vọng:
- Fail với lỗi validation rõ ràng (không phải timeout/x509).

---

## 8) “One-liners” (đỡ gõ, an toàn)

Repo có thể cung cấp (hoặc bạn có thể thêm) các target sau để tự động hoá:

- `make webhook-status`:
  - In deployments/pods/svc/endpoints/webhookconfigs
  - In log webhook startup

- `make webhook-smoke`:
  - Chạy 2 dry-run tests (valid + invalid)

---

## 9) Checklist an toàn (khuyến nghị cho môi trường thật)

- Webhook handler phải chạy nhanh, không gọi network lâu, tránh block.
- `timeoutSeconds` 5–10s là hợp lý.
- Dev: cân nhắc `failurePolicy: Ignore` khi debug để tránh “brick” admission.
- Prod: `failurePolicy: Fail` (an toàn) + đảm bảo webhook highly available.
- Tránh dùng `latest` nếu CI/CD nhiều môi trường (dễ cache/pull nhầm).
- Luôn kiểm tra `Endpoints/EndpointSlice` trước khi kết luận webhook “chết”.
