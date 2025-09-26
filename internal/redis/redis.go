package redis

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nextcloudv1 "github.com/LoriKarikari/nextcloud-operator/api/v1"
)

type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *Reconciler) ReconcileRedis(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Redis cache", "name", nc.Name, "namespace", nc.Namespace)

	if err := r.reconcileRedisConfigMap(ctx, nc); err != nil {
		return err
	}

	if err := r.reconcileRedisDeployment(ctx, nc); err != nil {
		return err
	}

	if err := r.reconcileRedisService(ctx, nc); err != nil {
		return err
	}

	return nil
}

func (r *Reconciler) reconcileRedisConfigMap(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	configMap := r.configMapForRedis(nc)

	found := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: configMap.Name, Namespace: configMap.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, configMap)
	} else if err != nil {
		return err
	}

	found.Data = configMap.Data
	return r.Update(ctx, found)
}

func (r *Reconciler) reconcileRedisDeployment(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	deployment := r.deploymentForRedis(nc)

	found := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, deployment)
	} else if err != nil {
		return err
	}

	if found.Spec.Template.Spec.Containers[0].Image != deployment.Spec.Template.Spec.Containers[0].Image {
		found.Spec.Template = deployment.Spec.Template
		return r.Update(ctx, found)
	}
	return nil
}

func (r *Reconciler) reconcileRedisService(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	service := r.serviceForRedis(nc)

	found := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, service)
	} else if err != nil {
		return err
	}

	found.Spec.Selector = service.Spec.Selector
	found.Spec.Ports = service.Spec.Ports
	return r.Update(ctx, found)
}

func (r *Reconciler) configMapForRedis(nc *nextcloudv1.Nextcloud) *corev1.ConfigMap {
	redisConfig := `maxmemory 256mb
maxmemory-policy allkeys-lru
save 900 1
save 300 10
save 60 10000
appendonly yes
appendfsync everysec
tcp-keepalive 60
timeout 300`

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name + "-redis-config",
			Namespace: nc.Namespace,
		},
		Data: map[string]string{
			"redis.conf": redisConfig,
		},
	}

	ctrl.SetControllerReference(nc, configMap, r.Scheme)
	return configMap
}

func (r *Reconciler) deploymentForRedis(nc *nextcloudv1.Nextcloud) *appsv1.Deployment {
	labels := map[string]string{
		"app":       "redis",
		"component": "cache",
		"instance":  nc.Name,
	}

	replicas := int32(1)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name + "-redis",
			Namespace: nc.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "redis",
						Image: "redis:7-alpine",
						Args: []string{
							"redis-server",
							"/etc/redis/redis.conf",
						},
						Ports: []corev1.ContainerPort{{
							ContainerPort: 6379,
							Name:          "redis",
						}},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "redis-config",
								MountPath: "/etc/redis/redis.conf",
								SubPath:   "redis.conf",
							},
							{
								Name:      "redis-data",
								MountPath: "/data",
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt(6379),
								},
							},
							InitialDelaySeconds: 5,
							PeriodSeconds:       5,
						},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt(6379),
								},
							},
							InitialDelaySeconds: 15,
							PeriodSeconds:       20,
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "redis-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: nc.Name + "-redis-config",
									},
								},
							},
						},
						{
							Name: "redis-data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	ctrl.SetControllerReference(nc, deployment, r.Scheme)
	return deployment
}

func (r *Reconciler) serviceForRedis(nc *nextcloudv1.Nextcloud) *corev1.Service {
	labels := map[string]string{
		"app":       "redis",
		"component": "cache",
		"instance":  nc.Name,
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name + "-redis",
			Namespace: nc.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Port:       6379,
				TargetPort: intstr.FromInt(6379),
				Name:       "redis",
			}},
			ClusterIP: "None",
		},
	}

	ctrl.SetControllerReference(nc, service, r.Scheme)
	return service
}
