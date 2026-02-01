package provider

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/SaladTechnologies/virtual-kubelet-saladcloud/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakePodsTrackerHandler struct {
	statusErr error
}

func (f *fakePodsTrackerHandler) GetPods(ctx context.Context) ([]*corev1.Pod, error) {
	return nil, nil
}

func (f *fakePodsTrackerHandler) GetPodStatus(ctx context.Context, namespace, name string) (*corev1.PodStatus, error) {
	return nil, f.statusErr
}

func (f *fakePodsTrackerHandler) DeletePod(ctx context.Context, pod *corev1.Pod) error {
	return nil
}

// Test 1: 404 on Pending pod transitions to Failed
func TestHandlePodUpdates_NotFoundPending(t *testing.T) {
	ctx := context.Background()
	pt := &PodsTracker{
		ctx:    ctx,
		logger: log.G(ctx),
		handler: &fakePodsTrackerHandler{
			statusErr: &models.APIError{StatusCode: http.StatusNotFound, Message: "not found"},
		},
	}

	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "pending-container",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "Pending"},
					},
				},
			},
		},
	}

	updated := pt.handlePodUpdates(pod)

	assert.True(t, updated)
	assert.Equal(t, corev1.PodFailed, pod.Status.Phase)
	assert.Equal(t, "NotFoundOnProvider", pod.Status.Reason)
	assert.Equal(t, "The container group has been deleted", pod.Status.Message)
	// Waiting container should remain Waiting (current behavior)
	assert.NotNil(t, pod.Status.ContainerStatuses[0].State.Waiting)
	assert.Nil(t, pod.Status.ContainerStatuses[0].State.Terminated)
}

// Test 2: 404 on Running pod transitions to Failed with container Terminated
func TestHandlePodUpdates_NotFoundRunning(t *testing.T) {
	ctx := context.Background()
	pt := &PodsTracker{
		ctx:    ctx,
		logger: log.G(ctx),
		handler: &fakePodsTrackerHandler{
			statusErr: &models.APIError{StatusCode: http.StatusNotFound, Message: "not found"},
		},
	}

	startTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "running-container",
					ContainerID: "container-123",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: startTime,
						},
					},
				},
			},
		},
	}

	updated := pt.handlePodUpdates(pod)

	assert.True(t, updated)
	assert.Equal(t, corev1.PodFailed, pod.Status.Phase)
	assert.Equal(t, "NotFoundOnProvider", pod.Status.Reason)
	assert.Equal(t, "The container group has been deleted", pod.Status.Message)
	// Running container should transition to Terminated
	assert.Nil(t, pod.Status.ContainerStatuses[0].State.Running)
	assert.NotNil(t, pod.Status.ContainerStatuses[0].State.Terminated)
	assert.Equal(t, int32(137), pod.Status.ContainerStatuses[0].State.Terminated.ExitCode)
	assert.Equal(t, "NotFoundOnProvider", pod.Status.ContainerStatuses[0].State.Terminated.Reason)
	assert.Equal(t, startTime, pod.Status.ContainerStatuses[0].State.Terminated.StartedAt)
}

// Test 3: 404 on Succeeded pod is ignored
func TestHandlePodUpdates_NotFoundSucceeded(t *testing.T) {
	ctx := context.Background()
	pt := &PodsTracker{
		ctx:    ctx,
		logger: log.G(ctx),
		handler: &fakePodsTrackerHandler{
			statusErr: &models.APIError{StatusCode: http.StatusNotFound, Message: "not found"},
		},
	}

	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "succeeded-container",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 0,
							Reason:   "Completed",
						},
					},
				},
			},
		},
	}

	updated := pt.handlePodUpdates(pod)

	assert.False(t, updated)
	assert.Equal(t, corev1.PodSucceeded, pod.Status.Phase)
	assert.Empty(t, pod.Status.Reason)
}

// Test 5: 404 on Failed pod is ignored
func TestHandlePodUpdates_NotFoundFailed(t *testing.T) {
	ctx := context.Background()
	pt := &PodsTracker{
		ctx:    ctx,
		logger: log.G(ctx),
		handler: &fakePodsTrackerHandler{
			statusErr: &models.APIError{StatusCode: http.StatusNotFound, Message: "not found"},
		},
	}

	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase:  corev1.PodFailed,
			Reason: "Error",
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "failed-container",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 1,
							Reason:   "Error",
						},
					},
				},
			},
		},
	}

	updated := pt.handlePodUpdates(pod)

	assert.False(t, updated)
	assert.Equal(t, corev1.PodFailed, pod.Status.Phase)
	assert.Equal(t, "Error", pod.Status.Reason)
}

// Test 6: 404 when DeletionTimestamp is set is ignored
func TestHandlePodUpdates_NotFoundDeletionTimestamp(t *testing.T) {
	ctx := context.Background()
	pt := &PodsTracker{
		ctx:    ctx,
		logger: log.G(ctx),
		handler: &fakePodsTrackerHandler{
			statusErr: &models.APIError{StatusCode: http.StatusNotFound, Message: "not found"},
		},
	}

	now := metav1.NewTime(time.Now())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			DeletionTimestamp: &now,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "terminating-container",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	updated := pt.handlePodUpdates(pod)

	assert.False(t, updated)
	assert.Equal(t, corev1.PodRunning, pod.Status.Phase)
	assert.NotNil(t, pod.Status.ContainerStatuses[0].State.Running)
}

// Test 7: Non-404 errors do not mutate pod
func TestHandlePodUpdates_Non404Error(t *testing.T) {
	ctx := context.Background()
	pt := &PodsTracker{
		ctx:    ctx,
		logger: log.G(ctx),
		handler: &fakePodsTrackerHandler{
			statusErr: &models.APIError{StatusCode: http.StatusInternalServerError, Message: "server error"},
		},
	}

	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "running-container",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	updated := pt.handlePodUpdates(pod)

	assert.False(t, updated)
	assert.Equal(t, corev1.PodRunning, pod.Status.Phase)
	assert.Empty(t, pod.Status.Reason)
	assert.NotNil(t, pod.Status.ContainerStatuses[0].State.Running)
}

// Test 8: Mixed container states - Running transitions, Waiting remains
func TestHandlePodUpdates_MixedContainerStates(t *testing.T) {
	ctx := context.Background()
	pt := &PodsTracker{
		ctx:    ctx,
		logger: log.G(ctx),
		handler: &fakePodsTrackerHandler{
			statusErr: &models.APIError{StatusCode: http.StatusNotFound, Message: "not found"},
		},
	}

	startTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "running-container",
					ContainerID: "container-123",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: startTime,
						},
					},
				},
				{
					Name: "waiting-container",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "Pending"},
					},
				},
			},
		},
	}

	updated := pt.handlePodUpdates(pod)

	assert.True(t, updated)
	assert.Equal(t, corev1.PodFailed, pod.Status.Phase)
	assert.Equal(t, "NotFoundOnProvider", pod.Status.Reason)

	// First container (Running) should be Terminated
	assert.Nil(t, pod.Status.ContainerStatuses[0].State.Running)
	assert.NotNil(t, pod.Status.ContainerStatuses[0].State.Terminated)
	assert.Equal(t, int32(137), pod.Status.ContainerStatuses[0].State.Terminated.ExitCode)

	// Second container (Waiting) should remain Waiting
	assert.NotNil(t, pod.Status.ContainerStatuses[1].State.Waiting)
	assert.Nil(t, pod.Status.ContainerStatuses[1].State.Terminated)
}
