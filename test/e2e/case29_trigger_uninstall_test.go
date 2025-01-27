// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/pkg/common"
	"open-cluster-management.io/config-policy-controller/pkg/triggeruninstall"
	"open-cluster-management.io/config-policy-controller/test/utils"
)

// This test only works when the controller is running in the cluster.
var _ = Describe("Clean up during uninstalls", Label("running-in-cluster"), Ordered, func() {
	const (
		configMapName        string = "case29-trigger-uninstall"
		deploymentName       string = "config-policy-controller"
		deploymentNamespace  string = "open-cluster-management-agent-addon"
		policyName           string = "case29-trigger-uninstall"
		policy2Name          string = "case29-trigger-uninstall2"
		policyYAMLPath       string = "../resources/case29_trigger_uninstall/policy.yaml"
		policy2YAMLPath      string = "../resources/case29_trigger_uninstall/policy2.yaml"
		pruneObjectFinalizer string = "policy.open-cluster-management.io/delete-related-objects"
	)

	It("verifies that finalizers are removed when being uninstalled", func() {
		By("Creating two configuration policies with pruneObjectBehavior")
		utils.Kubectl("apply", "-f", policyYAMLPath, "-n", testNamespace)
		utils.Kubectl("apply", "-f", policy2YAMLPath, "-n", testNamespace)

		By("Verifying that the configuration policies are compliant and have finalizers")
		Eventually(func(g Gomega) {
			policy := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)
			g.Expect(utils.GetComplianceState(policy)).To(Equal("Compliant"))

			g.Expect(policy.GetFinalizers()).To(ContainElement(pruneObjectFinalizer))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		Eventually(func(g Gomega) {
			policy2 := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policy2Name, testNamespace, true, defaultTimeoutSeconds,
			)
			g.Expect(utils.GetComplianceState(policy2)).To(Equal("Compliant"))

			g.Expect(policy2.GetFinalizers()).To(ContainElement(pruneObjectFinalizer))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Triggering an uninstall")
		config, err := LoadConfig("", kubeconfigManaged, "")
		Expect(err).To(BeNil())

		ctx, ctxCancel := context.WithDeadline(
			context.Background(),
			// Cancel the context after the default timeout seconds to avoid the test running forever if it doesn't
			// exit cleanly before then.
			time.Now().Add(time.Duration(defaultTimeoutSeconds)*time.Second),
		)
		defer ctxCancel()

		err = triggeruninstall.TriggerUninstall(ctx, config, deploymentName, deploymentNamespace, testNamespace)
		Expect(err).To(BeNil())

		By("Verifying that the uninstall annotation was set on the Deployment")
		deployment, err := clientManaged.AppsV1().Deployments(deploymentNamespace).Get(
			context.TODO(), deploymentName, metav1.GetOptions{},
		)
		Expect(err).To(BeNil())
		Expect(deployment.GetAnnotations()).To(HaveKeyWithValue(common.UninstallingAnnotation, "true"))

		By("Verifying that the ConfiguratioPolicy finalizers have been removed")
		policy := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(policy.GetFinalizers()).To(HaveLen(0))

		policy2 := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policy2Name, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(policy2.GetFinalizers()).To(HaveLen(0))
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName, policy2Name})

		err := clientManaged.CoreV1().ConfigMaps("default").Delete(
			context.TODO(), configMapName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}

		// Use an eventually in case there are update conflicts and there needs to be a retry
		Eventually(func(g Gomega) {
			deployment, err := clientManaged.AppsV1().Deployments(deploymentNamespace).Get(
				context.TODO(), deploymentName, metav1.GetOptions{},
			)
			g.Expect(err).To(BeNil())

			annotations := deployment.GetAnnotations()
			if _, ok := annotations[common.UninstallingAnnotation]; !ok {
				return
			}

			delete(annotations, common.UninstallingAnnotation)
			deployment.SetAnnotations(annotations)

			_, err = clientManaged.AppsV1().Deployments(deploymentNamespace).Update(
				context.TODO(), deployment, metav1.UpdateOptions{},
			)
			g.Expect(err).To(BeNil())
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})
})

// This test only works when the controller is running in the cluster.
var _ = Describe("Clean up the finalizer on the Deployment", Label("running-in-cluster"), Ordered, func() {
	const (
		deploymentName       string = "config-policy-controller"
		deploymentNamespace  string = "open-cluster-management-agent-addon"
		pruneObjectFinalizer string = "policy.open-cluster-management.io/delete-related-objects"
	)

	It("verifies that the Deployment finalizer is removed", func() {
		deploymentRsrc := clientManaged.AppsV1().Deployments(deploymentNamespace)

		By("Adding a finalizer to the Deployment")
		Eventually(func(g Gomega) {
			deployment, err := deploymentRsrc.Get(context.TODO(), deploymentName, metav1.GetOptions{})
			g.Expect(err).To(BeNil())

			deployment.SetFinalizers(append(deployment.Finalizers, pruneObjectFinalizer))
			_, err = deploymentRsrc.Update(context.TODO(), deployment, metav1.UpdateOptions{})
			g.Expect(err).To(BeNil())
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		// Trigger a restart so that the finalizer removal logic is executed.
		utils.Kubectl("-n", deploymentNamespace, "rollout", "restart", "deployment/"+deploymentName)

		By("Waiting for the finalizer on the Deployment to be removed")
		Eventually(func(g Gomega) {
			deployment, err := deploymentRsrc.Get(context.TODO(), deploymentName, metav1.GetOptions{})
			g.Expect(err).To(BeNil())

			g.Expect(deployment.Finalizers).ToNot(ContainElement(pruneObjectFinalizer))
		}, defaultTimeoutSeconds*2, 1).Should(Succeed())
	})

	AfterAll(func() {
		deploymentRsrc := clientManaged.AppsV1().Deployments(deploymentNamespace)

		// Use an eventually in case there are update conflicts and there needs to be a retry
		Eventually(func(g Gomega) {
			deployment, err := deploymentRsrc.Get(context.TODO(), deploymentName, metav1.GetOptions{})
			g.Expect(err).To(BeNil())

			for i, finalizer := range deployment.Finalizers {
				if finalizer == pruneObjectFinalizer {
					newFinalizers := append(deployment.Finalizers[:i], deployment.Finalizers[i+1:]...)
					deployment.SetFinalizers(newFinalizers)

					_, err := deploymentRsrc.Update(context.TODO(), deployment, metav1.UpdateOptions{})
					g.Expect(err).To(BeNil())

					break
				}
			}
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})
})
