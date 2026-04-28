package kubernetes

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Delete removes a container and its associated Kubernetes resources.
func (s *K8sContainerStore) Delete(ctx context.Context, containerID string) error {
	deploy, err := s.findDeployment(ctx, containerID)
	if err != nil {
		return err
	}

	propagation := metav1.DeletePropagationBackground

	// 2. Delete Service first (dependent resource, ignore if none found).
	svcs, err := s.client.CoreV1().Services(s.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: instanceSelector(containerID),
	})
	if err != nil {
		return err
	}
	for i := range svcs.Items {
		if delErr := s.client.CoreV1().Services(s.cfg.Namespace).Delete(ctx, svcs.Items[i].Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); delErr != nil {
			return delErr
		}
	}

	// 3. Delete Deployment (primary resource).
	// Use WithoutCancel so the Deployment deletion completes even if
	// the client disconnects after the Service is gone.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return s.client.AppsV1().Deployments(s.cfg.Namespace).Delete(cleanupCtx, deploy.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
}
