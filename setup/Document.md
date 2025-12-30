 
#  1. Tạo Tenant API:

````bash
kubebuilder create api --group platform --version v1alpha1 --kind Tenant --resource --controller
````

Sau đó:

````bash
make manifests
make install
kubectl get crd | grep -i tenant
kubectl api-resources | grep -i tenant
````
 



`kubectl get tenants` báo *“No resources found …”* nghĩa là:

 

## 1) Kiểm tra CRD (định nghĩa) đã có trong cluster chưa
```bash
kubectl get crd tenants.platform.shieldx.io
kubectl api-resources | grep -i tenant
```

Nếu có output → CRD ok.

## 2) Kiểm tra có Tenant object nào chưa (trong namespace cụ thể)
Tenant của bạn hiện là **namespaced** (vì `kubectl api-resources` có cột `NAMESPACED=true`).

- Xem trong `default`:
```bash
kubectl get tenants -n default
```

- Xem tất cả namespace:
```bash
kubectl get tenants -A
```

## 3) Tạo thử 1 Tenant để xác nhận “đã tạo xong” theo nghĩa có resource
Tạo file ví dụ:

````yaml
apiVersion: platform.shieldx.io/v1alpha1
kind: Tenant
metadata:
  name: payment-team
spec:
  owners:
    - "owner@shieldx.io"
  tier: "Gold"
  isolation: "Strict"
````

Apply:

```bash
kubectl apply -f tenant.yaml
kubectl get tenants -n default
kubectl get tenant payment-team -n default -o yaml
```

## 4) Nếu muốn kiểm tra controller có reconcile không (manager đã chạy)
```bash
kubectl get ns | grep tenant-payment-team
```
 

# 2. Trường hợp đúng với repo của bạn (Tenant Platform): tạo webhook cho `Tenant`
Nếu mục tiêu là validate/default `Tenant`, thì mới chạy kiểu này:

````bash
kubebuilder create webhook \
--group platform \
  --version v1alpha1 \
  --kind Tenant \
  --defaulting \
  --programmatic-validation
````

 
Cuối cùng deploy:

````bash
make deploy IMG=<your-image>
````

Kiểm tra đã đăng ký chưa:

````bash
kubectl get validatingwebhookconfigurations
kubectl get mutatingwebhookconfigurations
kubectl -n shieldx-platform-system get svc | grep webhook
````

 
# 3.  Đăng kí ValidatingWebhookConfiguration
Chỉnh `setup/webhook/ValidatingWebhookConfiguration.yaml` cho đúng với webhook server của bạn (xem phần diff đã sửa trong commit này).

Apply:
````bash
kubectl apply -f setup/webhook/ValidatingWebhookConfiguration.yaml
````


 # 4. Tự generate cert cho webhook server (nếu chưa có cert-manager)
Lỗi:
- `provider_conf_load... section=legacy_sect not found`

Đây là do file cấu hình OpenSSL hệ thống đang tham chiếu provider `legacy` nhưng thiếu section tương ứng.

### Workaround để generate cert (không đụng system config)
Chạy với `OPENSSL_CONF=/dev/null` để bỏ qua config hệ thống:

````bash
mkdir -p /tmp/k8s-webhook-server/serving-certs
OPENSSL_CONF=/dev/null openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout /tmp/k8s-webhook-server/serving-certs/tls.key \
  -out /tmp/k8s-webhook-server/serving-certs/tls.crt \
  -days 365 \
  -subj "/CN=localhost"
````

Sau đó chạy lại:

````bash
make run -- --webhook-cert-path=/tmp/k8s-webhook-server/serving-certs
````

---
 