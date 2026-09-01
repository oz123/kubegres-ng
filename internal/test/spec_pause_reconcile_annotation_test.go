/*
Copyright 2023 Reactive Tech Limited.
"Reactive Tech Limited" is a company located in England, United Kingdom.
https://www.reactive-tech.io

Lead Developer: Alex Arica

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

package test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"log"
	postgresv1 "reactive-tech.io/kubegres/api/v1"
	kubegresctx "reactive-tech.io/kubegres/internal/controller/ctx"
	resourceConfigs2 "reactive-tech.io/kubegres/internal/test/resourceConfigs"
	util2 "reactive-tech.io/kubegres/internal/test/util"
)

var _ = Describe("A Replica StatefulSet is annotated with pause-reconcile and its Pod becomes unavailable, checking Kubegres leaves it alone", func() {

	var test = SpecPauseReconcileAnnotationTest{}

	BeforeEach(func() {
		//Skip("Temporarily skipping test")

		namespace := resourceConfigs2.DefaultNamespace
		test.resourceRetriever = util2.CreateTestResourceRetriever(k8sClientTest, namespace)
		test.resourceCreator = util2.CreateTestResourceCreator(k8sClientTest, test.resourceRetriever, namespace)
	})

	AfterEach(func() {
		test.resourceCreator.DeleteAllTestResources()
	})

	Context("GIVEN Kubegres with 1 primary and 2 replicas AND one replica's StatefulSet is annotated with 'kubegres.reactive-tech.io/pause-reconcile=true' AND its Pod is deleted to simulate a node reboot", func() {

		It("THEN Kubegres should NOT undeploy/redeploy that replica's StatefulSet AND once the Pod is ready again the annotation should be automatically cleared", func() {

			log.Print("START OF: Test 'A Replica StatefulSet is annotated with pause-reconcile and its Pod becomes unavailable'")

			test.givenNewKubegresSpecIsSetTo(3)

			test.whenKubegresIsCreated()

			test.thenPodsStatesShouldBe(1, 2)

			pausedReplicaName := test.whenAReplicaStatefulSetIsAnnotatedWithPauseReconcile()

			test.whenThatReplicaPodIsDeleted(pausedReplicaName)

			test.thenPodsStatesShouldBe(1, 2)

			test.thenReplicaStatefulSetNameShouldStillBe(pausedReplicaName)

			test.thenReplicaStatefulSetShouldNotHavePauseReconcileAnnotation(pausedReplicaName)

			log.Print("END OF: Test 'A Replica StatefulSet is annotated with pause-reconcile and its Pod becomes unavailable'")
		})
	})
})

type SpecPauseReconcileAnnotationTest struct {
	kubegresResource  *postgresv1.Kubegres
	resourceCreator   util2.TestResourceCreator
	resourceRetriever util2.TestResourceRetriever
}

func (r *SpecPauseReconcileAnnotationTest) givenNewKubegresSpecIsSetTo(specNbreReplicas int32) {
	r.kubegresResource = resourceConfigs2.LoadKubegresYaml()
	r.kubegresResource.Spec.Replicas = &specNbreReplicas
}

func (r *SpecPauseReconcileAnnotationTest) whenKubegresIsCreated() {
	r.resourceCreator.CreateKubegres(r.kubegresResource)
}

// whenAReplicaStatefulSetIsAnnotatedWithPauseReconcile annotates the StatefulSet of one deployed
// replica with kubegresctx.PauseReconcileAnnotation and returns its name, so that Kubegres will
// leave it alone while it is not ready instead of undeploying/redeploying it.
func (r *SpecPauseReconcileAnnotationTest) whenAReplicaStatefulSetIsAnnotatedWithPauseReconcile() string {

	kubegresResources, err := r.resourceRetriever.GetKubegresResources()
	Expect(err).Should(Succeed())

	for _, kubegresResource := range kubegresResources.Resources {
		if kubegresResource.IsPrimary {
			continue
		}

		statefulSetToAnnotate := kubegresResource.StatefulSet.Resource
		if statefulSetToAnnotate.Annotations == nil {
			statefulSetToAnnotate.Annotations = map[string]string{}
		}
		statefulSetToAnnotate.Annotations[kubegresctx.PauseReconcileAnnotation] = "true"

		r.resourceCreator.UpdateResource(statefulSetToAnnotate, statefulSetToAnnotate.Name)
		log.Println("Annotated Replica StatefulSet '" + statefulSetToAnnotate.Name + "' with pause-reconcile=true")

		return statefulSetToAnnotate.Name
	}

	Fail("Could not find a Replica StatefulSet to annotate")
	return ""
}

// whenThatReplicaPodIsDeleted deletes the Pod owned by the given replica StatefulSet, simulating
// a Pod eviction caused by a voluntary node drain/reboot.
func (r *SpecPauseReconcileAnnotationTest) whenThatReplicaPodIsDeleted(replicaStatefulSetName string) {

	kubegresResources, err := r.resourceRetriever.GetKubegresResources()
	Expect(err).Should(Succeed())

	for _, kubegresResource := range kubegresResources.Resources {
		if kubegresResource.StatefulSet.Name != replicaStatefulSetName {
			continue
		}

		log.Println("Attempting to delete Pod: '" + kubegresResource.Pod.Name + "'")
		wasDeleted := r.resourceCreator.DeleteResource(kubegresResource.Pod.Resource, kubegresResource.Pod.Name)
		Expect(wasDeleted).Should(BeTrue())
		return
	}

	Fail("Could not find the Pod belonging to Replica StatefulSet '" + replicaStatefulSetName + "'")
}

// thenReplicaStatefulSetNameShouldStillBe asserts that, once the cluster is healthy again, a
// Replica StatefulSet with the given name is still deployed. Had Kubegres undeployed/redeployed
// it (the behaviour this annotation prevents), the replacement StatefulSet would have a different
// name, since Kubegres always allocates a new, never-reused instance index.
func (r *SpecPauseReconcileAnnotationTest) thenReplicaStatefulSetNameShouldStillBe(expectedName string) {

	kubegresResources, err := r.resourceRetriever.GetKubegresResources()
	Expect(err).Should(Succeed())

	for _, kubegresResource := range kubegresResources.Resources {
		if !kubegresResource.IsPrimary && kubegresResource.StatefulSet.Name == expectedName {
			log.Println("Replica StatefulSet '" + expectedName + "' was preserved as expected")
			return
		}
	}

	Fail("Replica StatefulSet '" + expectedName + "' is no longer deployed. It was likely undeployed " +
		"and replaced by Kubegres despite the pause-reconcile annotation.")
}

func (r *SpecPauseReconcileAnnotationTest) thenReplicaStatefulSetShouldNotHavePauseReconcileAnnotation(replicaStatefulSetName string) {
	Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		for _, kubegresResource := range kubegresResources.Resources {
			if kubegresResource.StatefulSet.Name != replicaStatefulSetName {
				continue
			}

			_, hasAnnotation := kubegresResource.StatefulSet.Metadata.Annotations[kubegresctx.PauseReconcileAnnotation]
			if hasAnnotation {
				log.Println("Replica StatefulSet '" + replicaStatefulSetName + "' still has the pause-reconcile annotation")
				return false
			}

			return true
		}

		return false

	}, resourceConfigs2.TestTimeout, resourceConfigs2.TestRetryInterval).Should(BeTrue())
}

func (r *SpecPauseReconcileAnnotationTest) thenPodsStatesShouldBe(nbrePrimary, nbreReplicas int) bool {
	return Eventually(func() bool {

		kubegresResources, err := r.resourceRetriever.GetKubegresResources()
		if err != nil && !apierrors.IsNotFound(err) {
			log.Println("ERROR while retrieving Kubegres kubegresResources")
			return false
		}

		if kubegresResources.AreAllReady &&
			kubegresResources.NbreDeployedPrimary == nbrePrimary &&
			kubegresResources.NbreDeployedReplicas == nbreReplicas {

			log.Println("Deployed and Ready StatefulSets check successful")
			return true
		}

		return false

	}, resourceConfigs2.TestTimeout, resourceConfigs2.TestRetryInterval).Should(BeTrue())
}
