package controllers_test_test

import (
	"context"
	"encoding/json"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sigs.k8s.io/cluster-api-provider-vsphere/pkg/manager"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient     client.Client
	testEnv       *envtest.Environment
	doneMgr       = make(chan struct{})
	ctx           = context.Background()
	isLB          = false
	clusterAPIDir = findModuleDir("sigs.k8s.io/cluster-api")
)

func init() {
	klog.InitFlags(nil)
	klog.SetOutput(GinkgoWriter)
	logf.SetLogger(klogr.New())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

//func TestAPIsLB(t *testing.T) {
//	isLB = true
//	RegisterFailHandler(Fail)
//
//	RunSpecsWithDefaultAndCustomReporters(t,
//		"Controller Suite",
//		[]Reporter{printer.NewlineReporter{}})
//}

func getTestEnv() (*envtest.Environment, *rest.Config) {
	localTestEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "config", "deployments", "local-with-vmop", "vmoperator", "config", "crd", "bases"),
			filepath.Join("..", "config", "deployments", "local-with-vmop", "netoperator", "config", "crd", "bases"),
			filepath.Join(clusterAPIDir, "config", "crd", "bases"),
		},
	}

	localCfg, err := localTestEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(localCfg).ToNot(BeNil())
	return localTestEnv, localCfg
}

func getManager(cfg *rest.Config) manager.Manager {
	mgr, err := manager.New(manager.Options{
		KubeConfig: cfg,
		Options: ctrlmgr.Options{
			Scheme: scheme.Scheme,
			NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
				syncPeriod := 1 * time.Second
				opts.Resync = &syncPeriod
				return cache.New(config, opts)
			},
		},
		MetricsAddr: "0",
	})
	Expect(err).NotTo(HaveOccurred())
	return mgr
}

// Create a NodeNetworkConfigMap in testEnvLB so that the manager will initialize with DummyLBNetworkProvider
func createNodeNetworkConfigMap(cfg *rest.Config) {
	name := manager.NodeNetworkCM.Name
	namespace := manager.NodeNetworkCM.Namespace
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	cm.Data = map[string]string{
		manager.DummyLBNetworkProviderCMKey: "true",
	}
	client := kubernetes.NewForConfigOrDie(cfg)
	_, err := client.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

var _ = BeforeSuite(func(done Done) {
	By("bootstrapping test environments")
	var cfg *rest.Config
	testEnv, cfg = getTestEnv()

	if isLB {
		By("Create LB Network ConfigMap")
		createNodeNetworkConfigMap(cfg)
	}

	By("setting up a new manager")
	mgr := getManager(cfg)
	k8sClient = mgr.GetClient()

	By("starting the manager")
	go func() {
		Expect(mgr.Start(ctx)).ToNot(HaveOccurred())
	}()

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environments")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func findModuleDir(module string) string {
	cmd := exec.Command("go", "mod", "download", "-json", module)
	out, err := cmd.Output()
	if err != nil {
		klog.Fatalf("Failed to run go mod to find module %q directory", module)
	}
	info := struct{ Dir string }{}
	if err := json.Unmarshal(out, &info); err != nil {
		klog.Fatalf("Failed to unmarshal output from go mod command: %v", err)
	} else if info.Dir == "" {
		klog.Fatalf("Failed to find go module %q directory, received %v", module, string(out))
	}
	return info.Dir
}
