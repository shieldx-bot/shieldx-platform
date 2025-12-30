package k8s

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	platformv1alpha1 "github.com/shieldx-bot/shieldx-platform/api/v1alpha1"
	"github.com/shieldx-bot/shieldx-platform/internal/webhook/notify"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var tenantlog = logf.Log.WithName("tenant-resource")

func GetClientset() (*kubernetes.Clientset, *rest.Config, error) {
	// Prefer in-cluster config (webhook runs inside Kubernetes).
	if cfg, err := rest.InClusterConfig(); err == nil {
		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create in-cluster clientset: %w", err)
		}
		return clientset, cfg, nil
	}

	// Fallback for local development: use kubeconfig.
	var kubeconfig *string
	home := homedir.HomeDir()
	if home != "" {
		defaultPath := filepath.Join(home, ".kube", "config")
		if kc := os.Getenv("KUBECONFIG"); kc != "" {
			defaultPath = kc
		}
		kubeconfig = flag.String("kubeconfig", defaultPath, "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute ")
	}
	if !flag.Parsed() {
		flag.Parse()
	}
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("không tìm thấy file kubeconfig: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("token/config không đúng: %w", err)
	}

	return clientset, config, nil
}

func CreateSecret(clientset *kubernetes.Clientset, namespace string, name string, data map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}

	_, err := clientset.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create secret: %w", err)
	}
	return nil
}

func CreateResource(clientset *kubernetes.Clientset, namespace string, resourceQuota platformv1alpha1.ResourceQuota) error {
	limitsCPU, err := resource.ParseQuantity(resourceQuota.LimitsCPU)
	if err != nil {
		return fmt.Errorf("invalid LimitsCPU %q: %w", resourceQuota.LimitsCPU, err)
	}
	limitsMemory, err := resource.ParseQuantity(resourceQuota.LimitsMemory)
	if err != nil {
		return fmt.Errorf("invalid LimitsMemory %q: %w", resourceQuota.LimitsMemory, err)
	}
	requestsCPU, err := resource.ParseQuantity(resourceQuota.RequestsCPU)
	if err != nil {
		return fmt.Errorf("invalid RequestsCPU %q: %w", resourceQuota.RequestsCPU, err)
	}
	requestsMemory, err := resource.ParseQuantity(resourceQuota.RequestsMemory)
	if err != nil {
		return fmt.Errorf("invalid RequestsMemory %q: %w", resourceQuota.RequestsMemory, err)
	}
	requestsStorage, err := resource.ParseQuantity(resourceQuota.RequestsStorage)
	if err != nil {
		return fmt.Errorf("invalid RequestsStorage %q: %w", resourceQuota.RequestsStorage, err)
	}
	pods, err := resource.ParseQuantity(resourceQuota.Pods)
	if err != nil {
		return fmt.Errorf("invalid Pods %q: %w", resourceQuota.Pods, err)
	}

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-resource-quota",
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:       limitsCPU,
				corev1.ResourceLimitsMemory:    limitsMemory,
				corev1.ResourceRequestsCPU:     requestsCPU,
				corev1.ResourceRequestsMemory:  requestsMemory,
				corev1.ResourceRequestsStorage: requestsStorage,
				corev1.ResourcePods:            pods,
			},
		},
	}

	_, err = clientset.CoreV1().ResourceQuotas(namespace).Create(context.TODO(), quota, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create resource quota: %w", err)
	}
	return nil
}

func CreateNetworkPolicy(clientset *kubernetes.Clientset, namespace string, networkPolicy platformv1alpha1.NetworkPolicy) error {
	// Map the project's simplified NetworkPolicy struct into the Kubernetes NetworkPolicy API.
	// NOTE: The custom type only supports Pod label selectors (same-namespace), no ports and no namespaceSelector.

	policyTypes := make([]netv1.PolicyType, 0, len(networkPolicy.PolicyTypes))
	for _, t := range networkPolicy.PolicyTypes {
		policyTypes = append(policyTypes, netv1.PolicyType(t))
	}

	ingressRules := make([]netv1.NetworkPolicyIngressRule, 0, len(networkPolicy.Ingress))
	for _, in := range networkPolicy.Ingress {
		peer := netv1.NetworkPolicyPeer{}
		// Empty selector means "all pods" (within the same namespace).
		peer.PodSelector = &metav1.LabelSelector{MatchLabels: in.From.Pod}
		ingressRules = append(ingressRules, netv1.NetworkPolicyIngressRule{
			From: []netv1.NetworkPolicyPeer{peer},
		})
	}

	egressRules := make([]netv1.NetworkPolicyEgressRule, 0, len(networkPolicy.Egress))
	for _, eg := range networkPolicy.Egress {
		peer := netv1.NetworkPolicyPeer{}
		peer.PodSelector = &metav1.LabelSelector{MatchLabels: eg.To.Pod}
		egressRules = append(egressRules, netv1.NetworkPolicyEgressRule{
			To: []netv1.NetworkPolicyPeer{peer},
		})
	}

	np := &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-network-policy",
			Namespace: namespace,
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: networkPolicy.PodSelector},
			PolicyTypes: policyTypes,
			Ingress:     ingressRules,
			Egress:      egressRules,
		},
	}

	_, err := clientset.NetworkingV1().NetworkPolicies(namespace).Create(context.TODO(), np, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create network policy: %w", err)
	}
	return nil
}

func CreateReconciliation(Name string, Tier string, Isolation string, Owners []string, ResourceQuota platformv1alpha1.ResourceQuota, NetworkPolicy platformv1alpha1.NetworkPolicy) error {
	clientset, _, err := GetClientset()
	if err != nil {
		fmt.Printf("Lỗi khi lấy clientset: %v\n", err)
		return err
	}
	// Create Namespace if Isolation is "namespace"
	if Isolation == "namespace" {
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tenant-" + Name,
			},
		}
		_, err = clientset.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
		if err != nil {
			err := notify.SendMessageTelegram("Lỗi khi tạo Namespace: " + err.Error())
			fmt.Printf("Lỗi khi tạo Namespace: %v\n", err)
			return err
		}
		tenantlog.Info("Namespace created", "name", Name)
	}
	// Create ResourceQuota
	err = CreateResource(clientset, "tenant-"+Name, ResourceQuota)
	if err != nil {
		err := notify.SendMessageTelegram("Lỗi khi tạo ResourceQuota: " + err.Error())
		fmt.Printf("Lỗi khi tạo ResourceQuota: %v\n", err)
		return err
	}
	tenantlog.Info("ResourceQuota created", "name", Name)

	// Create NetworkPolicy
	err = CreateNetworkPolicy(clientset, "tenant-"+Name, NetworkPolicy)
	if err != nil {
		err := notify.SendMessageTelegram("Lỗi khi tạo NetworkPolicy: " + err.Error())
		fmt.Printf("Lỗi khi tạo NetworkPolicy: %v\n", err)
		return err
	}
	tenantlog.Info("NetworkPolicy created", "name", Name)

	// Create Secret for Owners

	err = CreateSecret(clientset, "tenant-"+Name, "owners", map[string][]byte{
		"owners": []byte(fmt.Sprintf("%v", Owners)),
	})
	if err != nil {
		err := notify.SendMessageTelegram("Lỗi khi tạo Secret owners: " + err.Error())
		fmt.Printf("Lỗi khi tạo Secret owners: %v\n", err)
		return err
	}

	tenantlog.Info("Secret created for owners", "name", Name)

	Pods, err := clientset.CoreV1().Pods("shieldx-platform-system").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Println("Lỗi liệt kê nodes:", err)
		return err
	}

	for _, pod := range Pods.Items {

		err := notify.SendMessageTelegram("Pod Name: " + pod.Name + " - Status: " + string(pod.Status.Phase))
		if err != nil {
			fmt.Println("Lỗi gửi tin nhắn Telegram:", err)
			return err
		}
	}

	return nil

}

func DeleleteReconciliation(Name string) error {
	clientset, config, err := GetClientset()
	if err != nil {
		fmt.Printf("Lỗi khi lấy clientset: %v\n", err)
		return err
	}

	ctx := context.TODO()
	tenantGVR := schema.GroupVersionResource{
		Group:    "platform.shieldx.io",
		Version:  "v1alpha1",
		Resource: "tenants",
	}

	dc, err := dynamic.NewForConfig(config)
	if err != nil {
		fmt.Println("Lỗi tạo dynamic client:", err)
		return err
	}

	// Tenants may be namespaced; detect the namespace by listing and matching by name.
	var tenantCRNamespace string

	// Try cluster-scoped list first; if that fails (common for namespaced CRDs), fallback to list across all namespaces.
	tenantList, listErr := dc.Resource(tenantGVR).List(ctx, metav1.ListOptions{})
	if listErr != nil {
		tenantList, listErr = dc.Resource(tenantGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	}
	if listErr == nil {
		for i := range tenantList.Items {
			if tenantList.Items[i].GetName() == Name {
				tenantCRNamespace = tenantList.Items[i].GetNamespace()
				break
			}
		}
	}

	var tenantRes dynamic.ResourceInterface = dc.Resource(tenantGVR)
	if tenantCRNamespace != "" {
		tenantRes = dc.Resource(tenantGVR).Namespace(tenantCRNamespace)
	}

	// Best-effort remove finalizers (common cause of "cannot delete" / stuck deletion).
	if u, getErr := tenantRes.Get(ctx, Name, metav1.GetOptions{}); getErr == nil {
		if len(u.GetFinalizers()) > 0 {
			u.SetFinalizers(nil)
			_, _ = tenantRes.Update(ctx, u, metav1.UpdateOptions{})
		}
	}

	// Delete the Tenant CR (ignore NotFound).
	if err := tenantRes.Delete(ctx, Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		_ = notify.SendMessageTelegram("Lỗi khi xóa Tenant: " + err.Error())
		return fmt.Errorf("failed to delete Tenant %q: %w", Name, err)
	}

	tenantNS := "tenant-" + Name

	// Best-effort delete in-namespace resources.
	if err := clientset.NetworkingV1().NetworkPolicies(tenantNS).Delete(ctx, "tenant-network-policy", metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		_ = notify.SendMessageTelegram("Lỗi khi xóa NetworkPolicy: " + err.Error())
		return fmt.Errorf("failed to delete NetworkPolicy %s/%s: %w", tenantNS, "tenant-network-policy", err)
	}

	if err := clientset.CoreV1().ResourceQuotas(tenantNS).Delete(ctx, "tenant-resource-quota", metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		_ = notify.SendMessageTelegram("Lỗi khi xóa ResourceQuota: " + err.Error())
		return fmt.Errorf("failed to delete ResourceQuota %s/%s: %w", tenantNS, "tenant-resource-quota", err)
	}

	if err := clientset.CoreV1().Secrets(tenantNS).Delete(ctx, "owners", metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		_ = notify.SendMessageTelegram("Lỗi khi xóa Secret owners: " + err.Error())
		return fmt.Errorf("failed to delete Secret %s/%s: %w", tenantNS, "owners", err)
	}

	// Best-effort delete tenant namespace.
	if err := clientset.CoreV1().Namespaces().Delete(ctx, tenantNS, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		_ = notify.SendMessageTelegram("Lỗi khi xóa Namespace: " + err.Error())
		return fmt.Errorf("failed to delete namespace %q: %w", tenantNS, err)
	}

	tenantlog.Info("Reconciliation deleted", "name", Name)
	fmt.Println("Xóa tái cấu hình thành công")
	return nil
}
