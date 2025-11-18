package controller

import (
	rbgv1alpha1 "sigs.k8s.io/rbgs/api/workloads/v1alpha1"

	arksv1 "github.com/arks-ai/arks/api/v1"
)

func convertToRbgPodGroupPolicy(podGroupPolicy *arksv1.PodGroupPolicy) *rbgv1alpha1.PodGroupPolicy {
	var rbgPodGroupPolicy *rbgv1alpha1.PodGroupPolicy
	if podGroupPolicy != nil {
		rbgPodGroupPolicy = &rbgv1alpha1.PodGroupPolicy{}
		if podGroupPolicy.KubeScheduling != nil {
			rbgPodGroupPolicy.KubeScheduling = &rbgv1alpha1.KubeSchedulingPodGroupPolicySource{
				ScheduleTimeoutSeconds: podGroupPolicy.KubeScheduling.ScheduleTimeoutSeconds,
			}
		}
		if rbgPodGroupPolicy.VolcanoScheduling != nil {
			rbgPodGroupPolicy.VolcanoScheduling = &rbgv1alpha1.VolcanoSchedulingPodGroupPolicySource{
				PriorityClassName: podGroupPolicy.VolcanoScheduling.PriorityClassName,
				Queue:             podGroupPolicy.VolcanoScheduling.Queue,
			}
		}
	}
	return rbgPodGroupPolicy
}
