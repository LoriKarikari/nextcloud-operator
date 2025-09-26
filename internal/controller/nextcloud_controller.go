package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nextcloudv1 "github.com/LoriKarikari/nextcloud-operator/api/v1"
	"github.com/LoriKarikari/nextcloud-operator/internal/postgres"
	"github.com/LoriKarikari/nextcloud-operator/internal/redis"
)

type NextcloudReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	PostgresReconciler *postgres.Reconciler
	RedisReconciler    *redis.Reconciler
}

// +kubebuilder:rbac:groups=nextcloud.lorikarikari.io,resources=nextclouds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nextcloud.lorikarikari.io,resources=nextclouds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nextcloud.lorikarikari.io,resources=nextclouds/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *NextcloudReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var nextcloud nextcloudv1.Nextcloud
	if err := r.Get(ctx, req.NamespacedName, &nextcloud); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	finalizerName := "nextcloud.lorikarikari.io/finalizer"

	if nextcloud.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&nextcloud, finalizerName) {
			controllerutil.AddFinalizer(&nextcloud, finalizerName)
			return ctrl.Result{}, r.Update(ctx, &nextcloud)
		}
	} else {
		if controllerutil.ContainsFinalizer(&nextcloud, finalizerName) {
			if err := r.finalize(ctx, &nextcloud); err != nil {
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(&nextcloud, finalizerName)
			return ctrl.Result{}, r.Update(ctx, &nextcloud)
		}
		return ctrl.Result{}, nil
	}

	if err := r.reconcileSecret(ctx, &nextcloud); err != nil {
		logger.Error(err, "Failed to reconcile Secret")
		return ctrl.Result{}, err
	}

	if err := r.reconcilePVC(ctx, &nextcloud); err != nil {
		logger.Error(err, "Failed to reconcile PVC")
		return ctrl.Result{}, err
	}

	if err := r.PostgresReconciler.ReconcileDatabase(ctx, &nextcloud); err != nil {
		logger.Error(err, "Failed to reconcile Database")
		return ctrl.Result{}, err
	}

	if err := r.RedisReconciler.ReconcileRedis(ctx, &nextcloud); err != nil {
		logger.Error(err, "Failed to reconcile Redis")
		return ctrl.Result{}, err
	}

	if err := r.reconcileDeployment(ctx, &nextcloud); err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	if err := r.reconcileService(ctx, &nextcloud); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, &nextcloud); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NextcloudReconciler) deploymentForNextcloud(nc *nextcloudv1.Nextcloud) *appsv1.Deployment {
	replicas := int32(1)
	if nc.Spec.Replicas != nil {
		replicas = *nc.Spec.Replicas
	}

	labels := map[string]string{
		"app":     "nextcloud",
		"version": nc.Spec.Version,
	}

	env := r.getEnvironmentVariables(nc)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name,
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
						Image: "nextcloud:" + nc.Spec.Version,
						Name:  "nextcloud",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 80,
						}},
						Env: env,
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "nextcloud-data",
							MountPath: "/var/www/html",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "nextcloud-data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: nc.Name + "-data",
							},
						},
					}},
				},
			},
		},
	}

	ctrl.SetControllerReference(nc, deployment, r.Scheme)
	return deployment
}

func (r *NextcloudReconciler) serviceForNextcloud(nc *nextcloudv1.Nextcloud) *corev1.Service {
	labels := map[string]string{
		"app":     "nextcloud",
		"version": nc.Spec.Version,
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name,
			Namespace: nc.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Port:       80,
				TargetPort: intstr.FromInt(80),
			}},
		},
	}

	ctrl.SetControllerReference(nc, service, r.Scheme)
	return service
}

func (r *NextcloudReconciler) reconcileDeployment(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	deployment := r.deploymentForNextcloud(nc)

	found := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, deployment)
	} else if err != nil {
		return err
	}

	needsUpdate := false
	if found.Spec.Replicas == nil || *found.Spec.Replicas != *deployment.Spec.Replicas {
		needsUpdate = true
	}
	if found.Spec.Template.Spec.Containers[0].Image != deployment.Spec.Template.Spec.Containers[0].Image {
		needsUpdate = true
	}

	if len(found.Spec.Template.Spec.Containers[0].Env) != len(deployment.Spec.Template.Spec.Containers[0].Env) {
		needsUpdate = true
	} else {
		foundEnvMap := make(map[string]string)
		for _, env := range found.Spec.Template.Spec.Containers[0].Env {
			foundEnvMap[env.Name] = env.Value
		}
		for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
			if foundEnvMap[env.Name] != env.Value {
				needsUpdate = true
				break
			}
		}
	}

	if len(found.Spec.Template.Spec.Containers[0].VolumeMounts) != len(deployment.Spec.Template.Spec.Containers[0].VolumeMounts) {
		needsUpdate = true
	}

	if len(found.Spec.Template.Spec.Volumes) != len(deployment.Spec.Template.Spec.Volumes) {
		needsUpdate = true
	}

	if needsUpdate {
		found.Spec.Replicas = deployment.Spec.Replicas
		found.Spec.Template = deployment.Spec.Template
		found.Spec.Selector = deployment.Spec.Selector
		return r.Update(ctx, found)
	}
	return nil
}

func (r *NextcloudReconciler) reconcileService(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	service := r.serviceForNextcloud(nc)

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

func (r *NextcloudReconciler) updateStatus(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	latest := &nextcloudv1.Nextcloud{}
	if err := r.Get(ctx, client.ObjectKey{Name: nc.Name, Namespace: nc.Namespace}, latest); err != nil {
		return err
	}

	latest.Status.ObservedGeneration = nc.Generation

	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Name: nc.Name, Namespace: nc.Namespace}, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			r.setCondition(latest, "Available", metav1.ConditionFalse, "DeploymentNotFound", "Deployment not found")
			latest.Status.Phase = "Pending"
		} else {
			r.setCondition(latest, "Available", metav1.ConditionUnknown, "DeploymentCheckFailed", "Failed to check deployment status")
			latest.Status.Phase = "Pending"
		}
		return r.Status().Update(ctx, latest)
	}

	if deployment.Status.ReadyReplicas > 0 {
		r.setCondition(latest, "Available", metav1.ConditionTrue, "DeploymentReady", "Deployment is ready")
		r.setCondition(latest, "Progressing", metav1.ConditionFalse, "DeploymentComplete", "Deployment completed successfully")
		latest.Status.Phase = "Ready"
	} else {
		r.setCondition(latest, "Available", metav1.ConditionFalse, "DeploymentNotReady", "Deployment not ready")
		r.setCondition(latest, "Progressing", metav1.ConditionTrue, "DeploymentProgressing", "Deployment is progressing")
		latest.Status.Phase = "Installing"
	}

	return r.Status().Update(ctx, latest)
}

func (r *NextcloudReconciler) setCondition(nc *nextcloudv1.Nextcloud, condType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
	meta.SetStatusCondition(&nc.Status.Conditions, condition)
}

func (r *NextcloudReconciler) getEnvironmentVariables(nc *nextcloudv1.Nextcloud) []corev1.EnvVar {
	adminSecretName, _, adminPasswordKey := r.getAdminSecretInfo(nc)
	adminUsername := "admin"
	if nc.Spec.Admin != nil && nc.Spec.Admin.Username != "" {
		adminUsername = nc.Spec.Admin.Username
	}

	env := []corev1.EnvVar{
		{
			Name:  "NEXTCLOUD_ADMIN_USER",
			Value: adminUsername,
		},
		{
			Name: "NEXTCLOUD_ADMIN_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: adminSecretName,
					},
					Key: adminPasswordKey,
				},
			},
		},
		{
			Name:  "NEXTCLOUD_TRUSTED_DOMAINS",
			Value: "localhost *",
		},
	}

	env = append(env, []corev1.EnvVar{
		{
			Name:  "POSTGRES_HOST",
			Value: nc.Name + "-postgres",
		},
		{
			Name: "POSTGRES_DB",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nc.Name + "-postgres",
					},
					Key: "POSTGRES_DB",
				},
			},
		},
		{
			Name: "POSTGRES_USER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nc.Name + "-postgres",
					},
					Key: "POSTGRES_USER",
				},
			},
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: nc.Name + "-postgres",
					},
					Key: "POSTGRES_PASSWORD",
				},
			},
		},
	}...)

	env = append(env, []corev1.EnvVar{
		{
			Name:  "REDIS_HOST",
			Value: nc.Name + "-redis",
		},
		{
			Name:  "REDIS_HOST_PORT",
			Value: "6379",
		},
	}...)

	return env
}

func (r *NextcloudReconciler) getAdminSecretInfo(nc *nextcloudv1.Nextcloud) (secretName, usernameKey, passwordKey string) {
	if nc.Spec.Admin != nil && nc.Spec.Admin.SecretRef != nil {
		secretName = nc.Spec.Admin.SecretRef.Name
		usernameKey = nc.Spec.Admin.SecretRef.UsernameKey
		passwordKey = nc.Spec.Admin.SecretRef.PasswordKey
		if passwordKey == "" {
			passwordKey = "password"
		}
	} else {
		secretName = nc.Name + "-admin"
		passwordKey = "password"
	}
	return
}

func (r *NextcloudReconciler) reconcileSecret(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	if nc.Spec.Admin != nil && nc.Spec.Admin.SecretRef != nil {
		return nil
	}

	secretName := nc.Name + "-admin"
	secret := &corev1.Secret{}

	err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nc.Namespace}, secret)
	if err != nil && errors.IsNotFound(err) {
		adminUsername := "admin"
		if nc.Spec.Admin != nil && nc.Spec.Admin.Username != "" {
			adminUsername = nc.Spec.Admin.Username
		}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: nc.Namespace,
			},
			StringData: map[string]string{
				"username": adminUsername,
				"password": r.generatePassword(),
			},
		}
		ctrl.SetControllerReference(nc, secret, r.Scheme)
		return r.Create(ctx, secret)
	}
	return err
}

func (r *NextcloudReconciler) reconcilePVC(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	pvc := r.pvcForNextcloud(nc)

	found := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Name: pvc.Name, Namespace: pvc.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, pvc)
	}
	return err
}

func (r *NextcloudReconciler) pvcForNextcloud(nc *nextcloudv1.Nextcloud) *corev1.PersistentVolumeClaim {
	storageSize := "10Gi"
	if nc.Spec.StorageSize != "" {
		storageSize = nc.Spec.StorageSize
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name + "-data",
			Namespace: nc.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if nc.Spec.StorageClass != nil {
		pvc.Spec.StorageClassName = nc.Spec.StorageClass
	}

	ctrl.SetControllerReference(nc, pvc, r.Scheme)
	return pvc
}

func (r *NextcloudReconciler) generatePassword() string {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-password-" + string(bytes[:8])
	}
	return base64.URLEncoding.EncodeToString(bytes)[:32]
}

func (r *NextcloudReconciler) finalize(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	logger := log.FromContext(ctx)
	logger.Info("Finalizing Nextcloud", "name", nc.Name, "namespace", nc.Namespace)

	if nc.Spec.Admin == nil || nc.Spec.Admin.SecretRef == nil {
		secretName := nc.Name + "-admin"
		secret := &corev1.Secret{}
		err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nc.Namespace}, secret)
		if err == nil {
			logger.Info("Deleting auto-generated admin secret", "secret", secretName)
			if err := r.Delete(ctx, secret); err != nil {
				logger.Error(err, "Failed to delete admin secret", "secret", secretName)
				return err
			}
		} else if !errors.IsNotFound(err) {
			return err
		}
	}

	logger.Info("Finalization completed", "name", nc.Name, "namespace", nc.Namespace)
	return nil
}

func (r *NextcloudReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.PostgresReconciler = &postgres.Reconciler{
		Client: r.Client,
		Scheme: r.Scheme,
	}

	r.RedisReconciler = &redis.Reconciler{
		Client: r.Client,
		Scheme: r.Scheme,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&nextcloudv1.Nextcloud{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Named("nextcloud").
		Complete(r)
}
